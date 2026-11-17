package security

import (
	"context"
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	p, err := NewPasswords(Cost{Memory: 19 * 1024, Time: 2, Threads: 1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	password := "correct horse battery staple"
	first, err := p.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || strings.Contains(first, password) || !strings.HasPrefix(first, "$argon2id$") {
		t.Fatal("hashes should use fresh salts and contain no plaintext")
	}
	for _, tc := range []struct {
		password, hash string
		want           bool
	}{
		{password, first, true},
		{"wrong password", first, false},
		{password, "$argon2id$v=19$m=999999999,t=2,p=1$bad$bad", false},
		{password, "malformed", false},
	} {
		got, err := p.Verify(ctx, tc.password, tc.hash)
		if err != nil || got != tc.want {
			t.Fatalf("verification = %v, %v; want %v", got, err, tc.want)
		}
	}
}
