package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"reflect"
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

func TestCookieSignerCachesUntilHalfMaxAge(t *testing.T) {
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
	now := time.Unix(1_800_000_000, 0)
	first, err := signer.Cookies("google-sub", now)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := signer.Cookies("google-sub", now.Add(29*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cached, first) {
		t.Fatal("cookies were re-signed before half their max-age")
	}
	refreshed, err := signer.Cookies("google-sub", now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(refreshed, first) {
		t.Fatal("cookies were not re-signed at half their max-age")
	}
}

func TestClearContentCookiesMatchesSignedCookiePaths(t *testing.T) {
	cookies := ClearContentCookies("reader")
	seen := map[string]bool{}
	for _, raw := range cookies {
		cookie, err := http.ParseSetCookie(raw)
		if err != nil {
			t.Fatal(err)
		}
		if cookie.MaxAge != -1 || cookie.Value != "" || !cookie.HttpOnly || !cookie.Secure {
			t.Fatalf("cookie = %#v", cookie)
		}
		seen[cookie.Path+"|"+cookie.Name] = true
	}
	for _, prefix := range []string{"bodies", "media", "archive"} {
		for _, name := range []string{"CloudFront-Policy", "CloudFront-Signature", "CloudFront-Key-Pair-Id", "CloudFront-Hash-Algorithm"} {
			if !seen["/"+prefix+"/reader/|"+name] {
				t.Fatalf("missing %s %s", prefix, name)
			}
		}
	}
	if len(seen) != 12 {
		t.Fatal(seen)
	}
}
