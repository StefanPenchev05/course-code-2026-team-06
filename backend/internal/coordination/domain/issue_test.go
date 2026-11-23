package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestIssueDescription(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		valid             bool
	}{
		{"empty", "", "", false}, {"spaces", " \t\r\n", "", false},
		{"Unicode whitespace", "\u0085\u00a0\u1680\u2002\u2028\u202f\u205f\u3000", "", false},
		{"normal", "  A tutoring problem.\n", "A tutoring problem.", true},
		{"Unicode", " Élève — 学生 👩🏽‍🎓 ", "Élève — 学生 👩🏽‍🎓", true},
		{"limit", strings.Repeat("a", 2000), strings.Repeat("a", 2000), true},
		{"Unicode limit", strings.Repeat("é", 1000), strings.Repeat("é", 1000), true},
		{"too long", strings.Repeat("a", 2001), "", false},
		{"Unicode too long", strings.Repeat("é", 1001), "", false},
		{"NUL", "text\x00text", "", false}, {"invalid UTF8", "text\xff", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Text(tc.input)
			if tc.valid && (err != nil || got != tc.want) {
				t.Fatal(got, err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid description accepted", err)
			}
		})
	}
}
