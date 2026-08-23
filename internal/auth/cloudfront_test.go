package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func TestCookieSignerScopesCookiesToUserPrefixes(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := x509.MarshalPKCS8PrivateKey(key)
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	signer, err := NewCookieSigner(string(privatePEM), "K123", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := signer.Cookies("google-sub", time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 12 {
		t.Fatalf("got %d cookies, want 12", len(cookies))
	}
	joined := strings.Join(cookies, "\n")
	for _, expected := range []string{"Path=/bodies/google-sub/", "Path=/media/google-sub/", "Path=/archive/google-sub/", "CloudFront-Key-Pair-Id=K123", "CloudFront-Hash-Algorithm=SHA256", "HttpOnly"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("cookies do not contain %q", expected)
		}
	}
}
