package domain

import "testing"

func TestDeriveSearchTextUnicodeLowercase(t *testing.T) {
	got := DeriveSearchText("  İSTANBUL Σ", "CAFÉ  ")
	if got != "istanbul σ café" {
		t.Fatalf("DeriveSearchText() = %q", got)
	}
}
