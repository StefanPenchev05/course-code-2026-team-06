package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestRatingAndComment(t *testing.T) {
	for value := 1; value <= 5; value++ {
		got, err := (Input{Rating: value}).Normalize()
		if err != nil || got.Rating != value || got.Comment != "" {
			t.Fatal(got, err)
		}
	}
	for _, input := range []Input{{Rating: 0}, {Rating: 6}, {Rating: 3, Comment: strings.Repeat("é", 2001)}, {Rating: 3, Comment: "a\x00b"}} {
		if _, err := input.Normalize(); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	got, err := (Input{Rating: 3, Comment: "  Helpful  "}).Normalize()
	if err != nil || got.Comment != "Helpful" {
		t.Fatal(got, err)
	}
}
