package main

import "testing"

func TestSeedRequiresExplicitLocalTarget(t *testing.T) {
	if err := run(false); err == nil {
		t.Fatal("seed ran without explicit local confirmation")
	}
	for _, target := range []string{"", "://invalid", "postgres://user:secret@production.example/tutti"} {
		t.Run(target, func(t *testing.T) {
			t.Setenv("DATABASE_URL", target)
			t.Setenv("DEMO_PASSWORD", "LocalDemoPassword!")
			if err := run(true); err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
}

func TestSeedRequiresPassword(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/tutti")
	for _, password := range []string{"", "short"} {
		t.Setenv("DEMO_PASSWORD", password)
		if err := run(true); err == nil {
			t.Fatal("missing or weak demo password accepted")
		}
	}
}
