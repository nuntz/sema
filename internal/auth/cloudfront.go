package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
)

type CookieSigner struct {
	privateKey *rsa.PrivateKey
	keyPairID  string
	maxAge     time.Duration
}

func NewCookieSigner(privatePEM, keyPairID string, maxAge time.Duration) (*CookieSigner, error) {
	if strings.TrimSpace(privatePEM) == "" || strings.TrimSpace(keyPairID) == "" {
		return nil, nil
	}
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return nil, fmt.Errorf("decode CloudFront private key PEM")
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		key, _ = parsed.(*rsa.PrivateKey)
	} else if parsed, pkcs1Err := x509.ParsePKCS1PrivateKey(block.Bytes); pkcs1Err == nil {
		key = parsed
	}
	if key == nil {
		return nil, fmt.Errorf("CloudFront private key is not an RSA private key")
	}
	return &CookieSigner{privateKey: key, keyPairID: keyPairID, maxAge: maxAge}, nil
}

func (s *CookieSigner) Cookies(userID string, now time.Time) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	var cookies []string
	for _, prefix := range []string{"bodies", "media"} {
		path := fmt.Sprintf("/content/%s/%s/", prefix, userID)
		policy, err := s.policy("https://*"+path+"*", now.Add(s.maxAge))
		if err != nil {
			return nil, err
		}
		attributes := fmt.Sprintf("; Path=%s; Max-Age=%d; Secure; HttpOnly; SameSite=Strict", path, int(s.maxAge.Seconds()))
		cookies = append(cookies,
			"CloudFront-Policy="+policy.encoded+attributes,
			"CloudFront-Signature="+policy.signature+attributes,
			"CloudFront-Key-Pair-Id="+s.keyPairID+attributes,
			"CloudFront-Hash-Algorithm=SHA256"+attributes,
		)
	}
	return cookies, nil
}

type signedPolicy struct {
	encoded   string
	signature string
}

func (s *CookieSigner) policy(resource string, expires time.Time) (signedPolicy, error) {
	payload, err := json.Marshal(map[string]any{"Statement": []any{map[string]any{
		"Resource":  resource,
		"Condition": map[string]any{"DateLessThan": map[string]int64{"AWS:EpochTime": expires.Unix()}},
	}}})
	if err != nil {
		return signedPolicy{}, err
	}
	digest := sha256.Sum256(payload)
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return signedPolicy{}, err
	}
	return signedPolicy{encoded: cloudFrontBase64(payload), signature: cloudFrontBase64(signature)}, nil
}

func cloudFrontBase64(value []byte) string {
	encoded := base64.StdEncoding.EncodeToString(value)
	return strings.NewReplacer("+", "-", "=", "_", "/", "~").Replace(encoded)
}
