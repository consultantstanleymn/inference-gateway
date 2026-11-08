package model

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
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
	// so only identical batches match to 1e-4.
	for _, c := range cases {
		got, err := m.Run([][]int64{tokenize.Features(c.Text)})
		if err != nil {
			t.Fatal(err)
		}
		top := 0
		for j, p := range got[0] {
			if p > got[0][top] {
				top = j
			}
			if d := math.Abs(float64(p - c.Probs[j])); d > 1e-4 {
				t.Fatalf("%q class %d: go %v python %v diff %v", c.Text, j, p, c.Probs[j], d)
			}
		}
		if top != c.Top {
			t.Fatalf("%q: top %d, python %d", c.Text, top, c.Top)
		}
	}
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
