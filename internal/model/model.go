// Package model loads an ONNX classifier and runs batches through ONNX Runtime.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// Meta is the small file that sits next to model.onnx.
type Meta struct {
	Version     string   `json:"version"`
	SHA256      string   `json:"sha256"`
	Labels      []string `json:"labels"`
	MaxFeatures int      `json:"max_features"`
}

// Model is a loaded classifier. Run is safe for concurrent use.
type Model struct {
	Meta Meta
	sess *ort.DynamicAdvancedSession
}

var (
	envOnce sync.Once
	envErr  error
)

func initEnv(libPath string) error {
	envOnce.Do(func() {
		ort.SetSharedLibraryPath(libPath)
		envErr = ort.InitializeEnvironment()
	})
	return envErr
}

// Load reads dir/meta.json and dir/model.onnx, checks the SHA-256 and opens a session.
func Load(dir, libPath string, threads int) (*Model, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json")) //nolint:gosec // the model directory comes from operator configuration, not from a request
	if err != nil {
		return nil, fmt.Errorf("read meta: %w", err)
	}
	var meta Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("parse meta: %w", err)
	}
	onnxPath := filepath.Join(dir, "model.onnx")
	blob, err := os.ReadFile(onnxPath) //nolint:gosec // the model path comes from operator configuration, not from a request
	if err != nil {
		return nil, fmt.Errorf("read model: %w", err)
	}
	sum := sha256.Sum256(blob)
	if got := hex.EncodeToString(sum[:]); got != meta.SHA256 {
		return nil, fmt.Errorf("model checksum mismatch: file %s, meta %s", got, meta.SHA256)
	}
	if len(meta.Labels) == 0 {
		return nil, errors.New("meta has no labels")
	}
	if err := initEnv(libPath); err != nil {
		return nil, fmt.Errorf("init onnxruntime: %w", err)
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer func() { _ = opts.Destroy() }()
	if err := opts.SetIntraOpNumThreads(threads); err != nil {
		return nil, err
	}
	sess, err := ort.NewDynamicAdvancedSessionWithONNXData(blob, []string{"ids", "mask"}, []string{"probs"}, opts)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &Model{Meta: meta, sess: sess}, nil
}

// Run pads rows to the longest row, runs one ONNX call and returns one probability row per input.
func (m *Model) Run(rows [][]int64) ([][]float32, error) {
	b := len(rows)
	if b == 0 {
		return nil, nil
	}
	l := 1
	for _, r := range rows {
		l = max(l, len(r))
	}
	ids := make([]int64, b*l)
	mask := make([]float32, b*l)
	for i, r := range rows {
		copy(ids[i*l:], r)
		for j := range r {
			mask[i*l+j] = 1
		}
	}
	shape := ort.NewShape(int64(b), int64(l))
	idT, err := ort.NewTensor(shape, ids)
	if err != nil {
		return nil, err
	}
	defer func() { _ = idT.Destroy() }()
	mT, err := ort.NewTensor(shape, mask)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mT.Destroy() }()

	outputs := []ort.Value{nil}
	if err := m.sess.Run([]ort.Value{idT, mT}, outputs); err != nil {
		return nil, err
	}
	out, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		_ = outputs[0].Destroy()
		return nil, errors.New("unexpected output type")
	}
	defer func() { _ = out.Destroy() }()
	data := out.GetData()
	n := len(m.Meta.Labels)
	if len(data) != b*n {
		return nil, fmt.Errorf("output size %d, want %d", len(data), b*n)
	}
	res := make([][]float32, b)
	for i := range res {
		res[i] = append([]float32(nil), data[i*n:(i+1)*n]...)
	}
	return res, nil
}

// Close frees the session.
func (m *Model) Close() error { return m.sess.Destroy() }
