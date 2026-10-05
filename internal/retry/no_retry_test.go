package retry

import (
	"errors"
	"fmt"
	"testing"
)

func TestRetryDecision(t *testing.T) {
	base := errors.New("temporary failure")
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", want: false},
		{name: "unmarked error", err: base, want: true},
		{name: "marked error", err: NoRetry(base), want: false},
		{name: "wrapped marked error", err: fmt.Errorf("request failed: %w", NoRetry(base)), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldRetry(tc.err); got != tc.want {
				t.Fatalf("ShouldRetry(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

func TestNoRetryPreservesCauseAndIsIdempotent(t *testing.T) {
	cause := errors.New("unsupported format")
	marked := NoRetry(cause)
	if !errors.Is(marked, cause) {
		t.Fatal("NoRetry should preserve the original error")
	}
	if got := NoRetry(marked); got != marked {
		t.Fatal("NoRetry should not wrap an already marked error")
	}
}
