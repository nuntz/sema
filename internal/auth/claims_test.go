package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/store"
)

type fakeSessionStore struct {
	records map[string]store.Session
	putHash string
	renewed int
	deleted int
}

func (f *fakeSessionStore) PutSession(_ context.Context, hash string, session store.Session) error {
	if f.records == nil {
		f.records = make(map[string]store.Session)
	}
	f.putHash = hash
	f.records[hash] = session
	return nil
}

func (f *fakeSessionStore) Session(_ context.Context, hash string) (store.Session, error) {
	session, ok := f.records[hash]
	if !ok {
		return store.Session{}, store.ErrNotFound
	}
	return session, nil
}

func (f *fakeSessionStore) RenewSession(_ context.Context, hash string, renewedAt, expiresAt int64) error {
	session, ok := f.records[hash]
	if !ok || session.TTL <= renewedAt {
		return store.ErrNotFound
	}
	session.RenewedAt, session.ExpiresAt, session.TTL = renewedAt, expiresAt, expiresAt
	f.records[hash] = session
	f.renewed++
	return nil
}

func (f *fakeSessionStore) DeleteSession(_ context.Context, hash string) error {
	delete(f.records, hash)
	f.deleted++
	return nil
}

func TestCreateStoresHashAndSetsFirstPartyCookie(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	raw := bytes.Repeat([]byte{0x42}, 32)
	repository := &fakeSessionStore{}
	sessions := NewSessions(repository)
	sessions.now, sessions.random = func() time.Time { return now }, bytes.NewReader(raw)

	cookie, err := sessions.Create(context.Background(), Claims{Subject: " user ", Email: " reader@example.com "})
	if err != nil {
		t.Fatal(err)
	}
	id := baseCookieValue(t, cookie)
	if id == repository.putHash || strings.Contains(cookie, repository.putHash) {
		t.Fatal("cookie exposed the stored session hash")
	}
	if repository.putHash != sessionHash(raw) {
		t.Fatalf("stored hash = %q", repository.putHash)
	}
	record := repository.records[repository.putHash]
	if record.Subject != "user" || record.Email != "reader@example.com" || record.CreatedAt != now.Unix() || record.RenewedAt != now.Unix() || record.ExpiresAt != now.Add(SessionLifetime).Unix() || record.TTL != record.ExpiresAt {
		t.Fatalf("session record = %#v", record)
	}
	parsed, err := http.ParseSetCookie(cookie)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != SessionCookieName || parsed.Value != id || parsed.Path != "/api" || parsed.MaxAge != int(SessionLifetime.Seconds()) || !parsed.Secure || !parsed.HttpOnly || parsed.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %#v", parsed)
	}
}

func TestFromRequestCookieValidation(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	validRaw := bytes.Repeat([]byte{0x23}, 32)
	validID := rawSessionID(validRaw)
	valid := store.Session{
		Subject: "reader", Email: "reader@example.com", CreatedAt: now.Add(-time.Hour).Unix(), RenewedAt: now.Add(-time.Hour).Unix(), ExpiresAt: now.Add(time.Hour).Unix(), TTL: now.Add(time.Hour).Unix(),
	}

	for _, test := range []struct {
		name    string
		cookies []string
		records map[string]store.Session
		ok      bool
		deleted int
	}{
		{name: "valid", cookies: []string{"theme=dark", SessionCookieName + "=" + validID}, records: map[string]store.Session{sessionHash(validRaw): valid}, ok: true},
		{name: "missing", cookies: []string{"theme=dark"}},
		{name: "expired", cookies: []string{SessionCookieName + "=" + validID}, records: map[string]store.Session{sessionHash(validRaw): withExpiry(valid, now.Add(-time.Second))}, deleted: 1},
		{name: "tampered", cookies: []string{SessionCookieName + "=" + rawSessionID(bytes.Repeat([]byte{0x24}, 32))}, records: map[string]store.Session{sessionHash(validRaw): valid}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeSessionStore{records: test.records}
			sessions := NewSessions(repository)
			sessions.now = func() time.Time { return now }
			claims, renewed, err := FromRequest(context.Background(), events.APIGatewayV2HTTPRequest{Cookies: test.cookies}, sessions)
			if test.ok {
				if err != nil || claims.Subject != "reader" || claims.Email != "reader@example.com" || renewed != "" {
					t.Fatalf("FromRequest = %#v, %q, %v", claims, renewed, err)
				}
			} else if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("error = %v, want unauthorized", err)
			}
			if repository.deleted != test.deleted {
				t.Fatalf("deletes = %d, want %d", repository.deleted, test.deleted)
			}
		})
	}
}

func TestFromRequestRenewsAtMostDaily(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	raw := bytes.Repeat([]byte{0x7a}, 32)
	hash := sessionHash(raw)
	repository := &fakeSessionStore{records: map[string]store.Session{hash: {
		Subject: "reader", CreatedAt: now.Add(-48 * time.Hour).Unix(), RenewedAt: now.Add(-25 * time.Hour).Unix(), ExpiresAt: now.Add(5 * 24 * time.Hour).Unix(), TTL: now.Add(5 * 24 * time.Hour).Unix(),
	}}}
	sessions := NewSessions(repository)
	sessions.now = func() time.Time { return now }
	request := events.APIGatewayV2HTTPRequest{Cookies: []string{SessionCookieName + "=" + rawSessionID(raw)}}

	if _, cookie, err := FromRequest(context.Background(), request, sessions); err != nil || cookie == "" || repository.renewed != 1 {
		t.Fatalf("first renewal = cookie %q, renewals %d, err %v", cookie, repository.renewed, err)
	}
	if repository.records[hash].ExpiresAt != now.Add(SessionLifetime).Unix() {
		t.Fatalf("expiry = %d", repository.records[hash].ExpiresAt)
	}
	if _, cookie, err := FromRequest(context.Background(), request, sessions); err != nil || cookie != "" || repository.renewed != 1 {
		t.Fatalf("second lookup = cookie %q, renewals %d, err %v", cookie, repository.renewed, err)
	}
}

func withExpiry(session store.Session, expiry time.Time) store.Session {
	session.ExpiresAt, session.TTL = expiry.Unix(), expiry.Unix()
	return session
}

func rawSessionID(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func baseCookieValue(t *testing.T, value string) string {
	t.Helper()
	cookie, err := http.ParseSetCookie(value)
	if err != nil {
		t.Fatal(err)
	}
	return cookie.Value
}
