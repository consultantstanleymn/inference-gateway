package server

import (
	"errors"
	"fmt"
	"testing"
)

func TestUnknownOutcomeMapsToOther(t *testing.T) {
	if got := outcome(nil); got != "ok" {
		t.Fatalf("outcome(nil) = %q, want ok", got)
	}
	if got := outcome(errors.New("surprise")); got != "error" {
		t.Fatalf("outcome(unknown) = %q, want error", got)
	}
	wrapped := fmt.Errorf("wrap: %w", errors.New("surprise"))
	if got := outcome(wrapped); got != "error" {
		t.Fatalf("outcome(wrapped unknown) = %q, want error", got)
	}
}
