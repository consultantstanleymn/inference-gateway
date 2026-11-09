package model

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/consultantstanleymn/inference-gateway/internal/tokenize"
)

func repoPath(t *testing.T, rel string) string {
	t.Helper()
	p := filepath.Join("..", "..", rel)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("missing %s (run scripts/fetch-onnxruntime.sh): %v", rel, err)
	}
	return p
}

// TestParityWithPython checks Go inference against Python ONNX Runtime on 200 samples.
func TestParityWithPython(t *testing.T) {
	lib := repoPath(t, "third_party/libonnxruntime.so")
	m, err := Load(repoPath(t, "models/v1"), lib, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()

	raw, err := os.ReadFile("testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text  string    `json:"text"`
		Top   int       `json:"top"`
		Probs []float32 `json:"probs"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 200 {
		t.Fatalf("want 200 cases, got %d", len(cases))
	}
	// One row per call, like the Python fixture. Dynamic int8 scales activations per batch,
	// so only identical batches match closely. CPUs also differ in int8 kernels (see ADR 0004).
	// The test therefore checks three things: a hard cap on the largest difference, a tight bound
	// on the 99th percentile over all probabilities, and full agreement on the top intents.
	var diffs []float64
	for _, c := range cases {
		got, err := m.Run([][]int64{tokenize.Features(c.Text)})
		if err != nil {
			t.Fatal(err)
		}
		if len(got[0]) != len(c.Probs) {
			t.Fatalf("%q: %d classes, python %d", c.Text, len(got[0]), len(c.Probs))
		}
		for j, p := range got[0] {
			d := math.Abs(float64(p - c.Probs[j]))
			if d > parityMaxAbs {
				t.Fatalf("%q class %d: go %v python %v diff %v above cap %v", c.Text, j, p, c.Probs[j], d, parityMaxAbs)
			}
			diffs = append(diffs, d)
		}
		goTop, pyTop := topK(got[0], 3), topK(c.Probs, 3)
		if goTop[0] != c.Top || pyTop[0] != c.Top {
			t.Fatalf("%q: top-1 go %d, python %d, fixture %d", c.Text, goTop[0], pyTop[0], c.Top)
		}
		// Top-3 as sets. A class may differ only if its Python probability is within the cap
		// of the Python third place (a near tie, where int8 noise can swap the order).
		in := map[int]bool{}
		for _, k := range pyTop {
			in[k] = true
		}
		for _, k := range goTop {
			if !in[k] && float64(c.Probs[pyTop[2]]-c.Probs[k]) > parityMaxAbs {
				t.Fatalf("%q: go top-3 %v, python top-3 %v", c.Text, goTop, pyTop)
			}
		}
	}
	sort.Float64s(diffs)
	if p99 := diffs[len(diffs)*99/100]; p99 > parityP99 {
		t.Fatalf("p99 diff %v above %v", p99, parityP99)
	}
}

// Parity bounds, justified in docs/engagement/adr/0004-quantized-parity-bound.md.
const (
	parityMaxAbs = 5e-3 // 3x the largest cross-CPU difference seen in CI (1.53e-3)
	parityP99    = 5e-4 // the old per-value bound, now applied to the 99th percentile
)

// topK returns the indexes of the k largest values, largest first. Ties go to the lower index.
func topK(p []float32, k int) []int {
	idx := make([]int, len(p))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return p[idx[a]] > p[idx[b]] })
	return idx[:k]
}

// TestBatchCompositionEffect documents a real property of dynamic int8: the same text can get
// slightly different probabilities in a different batch. The top intent must stay the same.
func TestBatchCompositionEffect(t *testing.T) {
	lib := repoPath(t, "third_party/libonnxruntime.so")
	m, err := Load(repoPath(t, "models/v1"), lib, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	raw, err := os.ReadFile("testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text string `json:"text"`
		Top  int    `json:"top"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	rows := make([][]int64, len(cases))
	for i, c := range cases {
		rows[i] = tokenize.Features(c.Text)
	}
	got, err := m.Run(rows) // one batch of 200
	if err != nil {
		t.Fatal(err)
	}
	same := 0
	for i, c := range cases {
		top := 0
		for j, p := range got[i] {
			if p > got[i][top] {
				top = j
			}
		}
		if top == c.Top {
			same++
		}
	}
	t.Logf("top intent unchanged in a batch of 200: %d of %d", same, len(cases))
	if same < len(cases)*98/100 {
		t.Fatalf("only %d of %d top intents match", same, len(cases))
	}
}

func TestChecksumMismatchFails(t *testing.T) {
	lib := repoPath(t, "third_party/libonnxruntime.so")
	dir := t.TempDir()
	src := repoPath(t, "models/v1")
	for _, f := range []string{"model.onnx", "meta.json"} {
		b, _ := os.ReadFile(filepath.Join(src, f))
		if f == "model.onnx" {
			b[len(b)-1] ^= 0xff
		}
		_ = os.WriteFile(filepath.Join(dir, f), b, 0o600)
	}
	if _, err := Load(dir, lib, 1); err == nil {
		t.Fatal("tampered model must not load")
	}
}
