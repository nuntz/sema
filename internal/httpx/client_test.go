package httpx

import (
	"net/netip"
	"testing"
)

func TestSafeIP(t *testing.T) {
	tests := map[string]bool{
		"8.8.8.8": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "10.0.0.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "192.0.2.1": false, "::1": false, "fc00::1": false,
	}
	for raw, want := range tests {
		if got := safeIP(netip.MustParseAddr(raw)); got != want {
			t.Errorf("safeIP(%s) = %v, want %v", raw, got, want)
		}
	}
}
