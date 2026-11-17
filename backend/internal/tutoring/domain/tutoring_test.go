package domain

import (
	"errors"
	"testing"
)

func TestGrade(t *testing.T) {
	for raw, want := range map[string]string{"15": "15.00", "20.00": "20.00", "17.5": "17.50", "19.99": "19.99"} {
		g, err := ParseGrade(raw)
		if err != nil || g.String() != want {
			t.Errorf("%q: got %v, %v; want %s", raw, g, err, want)
		}
	}
	for _, raw := range []string{"14.99", "20.01", "19.999", "20.001", "1.75e1", "\"17.5\"", "null"} {
		if _, err := ParseGrade(raw); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: got %v", raw, err)
		}
	}
}
