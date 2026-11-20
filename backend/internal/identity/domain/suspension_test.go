package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestSuspensionRules(t *testing.T) {
	for _, text := range []string{"", " \t\u2003", "a\x00b", string([]byte{0xff}), strings.Repeat("é", 1001)} {
		if _, err := SuspensionReason(text); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid reason accepted")
		}
	}
	if got, err := SuspensionReason("  " + strings.Repeat("é", 1000) + "  "); err != nil || len(got) != 2000 {
		t.Fatal(got, err)
	}
	for _, suspended := range []bool{false, true} {
		p := Principal{ID: "target", Student: true, Suspended: suspended}
		if err := p.ChangeSuspension(!suspended); err != nil {
			t.Fatal(err)
		}
		if err := p.ChangeSuspension(suspended); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
		p.Closed = true
		if err := p.ChangeSuspension(!suspended); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if err := (Principal{ID: "coordinator only"}).ChangeSuspension(true); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
