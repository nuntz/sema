package send

import "testing"

func TestSignMatchesTheDocumentedHMAC(t *testing.T) {
	// Reference value from:
	// printf '%s' '1790445851.{"version":1,"event":"ping"}' | openssl dgst -sha256 -hmac 'test-secret-0123456789abcdef0123456789'
	got := Sign("test-secret-0123456789abcdef0123456789", 1790445851, []byte(`{"version":1,"event":"ping"}`))
	want := "v1=cb1b0fc444a721869e589679fc9901efefc4d55f9c526871a94ad2bf2a5b6364"
	if got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
}
