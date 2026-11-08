package tokenize

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// TestGolden checks that Go produces the same ids as Python for 500 texts.
func TestGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text string  `json:"text"`
		IDs  []int64 `json:"ids"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 500 {
		t.Fatalf("want 500 golden cases, got %d", len(cases))
	}
	for _, c := range cases {
		if got := Features(c.Text); !slices.Equal(got, c.IDs) {
			t.Fatalf("text %q: got %v want %v", c.Text, got, c.IDs)
		}
	}
}

func TestLimits(t *testing.T) {
	long := ""
	for range 200 {
		long += "word "
	}
	if n := len(Features(long)); n > MaxFeatures {
		t.Fatalf("got %d features, max %d", n, MaxFeatures)
	}
	if len(Features("")) != 0 || len(Features("!!! ...")) != 0 {
		t.Fatal("text without words must give no features")
	}
}
