package model

import (
	"fmt"
	"testing"

	"github.com/consultantstanleymn/inference-gateway/internal/tokenize"
)

// BenchmarkRun shows how the cost per row falls as the batch grows (one ONNX call per batch).
func BenchmarkRun(b *testing.B) {
	m, err := Load("../../models/v1", "../../third_party/libonnxruntime.so", 1)
	if err != nil {
		b.Skip(err)
	}
	defer func() { _ = m.Close() }()
	row := tokenize.Features("Why was my transfer charged a fee when I topped up my card")
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64} {
		rows := make([][]int64, n)
		for i := range rows {
			rows[i] = row
		}
		b.Run(fmt.Sprintf("batch=%d", n), func(b *testing.B) {
			for b.Loop() {
				if _, err := m.Run(rows); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/row")
		})
	}
}
