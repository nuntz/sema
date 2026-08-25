package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/store"
)

const (
	SessionCookieName = "sema_session"
	SessionLifetime   = 30 * 24 * time.Hour
	SessionRenewAfter = 24 * time.Hour
)

var ErrUnauthorized = errors.New("unauthorized")

type Claims struct {
	Subject string
	Email   string
}

type sessionStore interface {
	PutSession(context.Context, string, store.Session) error
	Session(context.Context, string) (store.Session, error)
	RenewSession(context.Context, string, int64, int64) error
	DeleteSession(context.Context, string) error
}

type Sessions struct {
	store  sessionStore
	now    func() time.Time
	random io.Reader
}

func NewSessions(repository sessionStore) *Sessions {
	return &Sessions{store: repository, now: time.Now, random: rand.Reader}
}

func (s *Sessions) Create(ctx context.Context, claims Claims) (string, error) {
	claims.Subject = strings.TrimSpace(claims.Subject)
	claims.Email = strings.TrimSpace(claims.Email)
	if claims.Subject == "" {
		return "", ErrUnauthorized
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now().UTC().Truncate(time.Second)
	expires := now.Add(SessionLifetime).Unix()
	if err := s.store.PutSession(ctx, sessionHash(raw), store.Session{
		Subject: claims.Subject, Email: claims.Email, CreatedAt: now.Unix(), RenewedAt: now.Unix(), ExpiresAt: expires, TTL: expires,
	}); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}
	return sessionCookie(id), nil
}

// FromRequest resolves the first-party cookie and returns a replacement cookie
// only when the session's sliding expiration was renewed.
func FromRequest(ctx context.Context, request events.APIGatewayV2HTTPRequest, sessions *Sessions) (Claims, string, error) {
	id, err := sessionID(request)
	if err != nil || sessions == nil || sessions.store == nil {
		return Claims{}, "", ErrUnauthorized
	}
	raw, err := decodeSessionID(id)
	if err != nil {
		return Claims{}, "", ErrUnauthorized
	}
	hash := sessionHash(raw)
	record, err := sessions.store.Session(ctx, hash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Claims{}, "", ErrUnauthorized
		}
		return Claims{}, "", fmt.Errorf("look up session: %w", err)
	}
	now := sessions.now().UTC().Truncate(time.Second)
	if strings.TrimSpace(record.Subject) == "" || record.ExpiresAt <= now.Unix() || record.TTL <= now.Unix() {
		_ = sessions.store.DeleteSession(ctx, hash)
		return Claims{}, "", ErrUnauthorized
	}
	lastRenewal := record.RenewedAt
	if lastRenewal == 0 {
		lastRenewal = record.CreatedAt
	}
	renewedCookie := ""
	if now.Sub(time.Unix(lastRenewal, 0)) > SessionRenewAfter {
		expires := now.Add(SessionLifetime).Unix()
		if err := sessions.store.RenewSession(ctx, hash, now.Unix(), expires); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return Claims{}, "", ErrUnauthorized
			}
			return Claims{}, "", fmt.Errorf("renew session: %w", err)
		}
		renewedCookie = sessionCookie(id)
	}
	return Claims{Subject: strings.TrimSpace(record.Subject), Email: strings.TrimSpace(record.Email)}, renewedCookie, nil
}

func (s *Sessions) DeleteRequest(ctx context.Context, request events.APIGatewayV2HTTPRequest) error {
	id, err := sessionID(request)
	if err != nil {
		return nil
	}
	raw, err := decodeSessionID(id)
	if err != nil {
		return nil
	}
	return s.store.DeleteSession(ctx, sessionHash(raw))
}

func ClearSessionCookie() string {
	return (&http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/api", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}).String()
}

func sessionCookie(id string) string {
	return (&http.Cookie{
		Name: SessionCookieName, Value: id, Path: "/api", MaxAge: int(SessionLifetime.Seconds()), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}).String()
}

func sessionID(request events.APIGatewayV2HTTPRequest) (string, error) {
	httpRequest := &http.Request{Header: make(http.Header)}
	for _, value := range request.Cookies {
		httpRequest.Header.Add("Cookie", value)
	}
	value := ""
	for _, cookie := range httpRequest.Cookies() {
		if cookie.Name != SessionCookieName {
			continue
		}
		if value != "" {
			return "", ErrUnauthorized
		}
		value = cookie.Value
	}
	if value == "" {
		return "", ErrUnauthorized
	}
	return value, nil
}

func decodeSessionID(id string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != id {
		return nil, ErrUnauthorized
	}
	return raw, nil
}

func sessionHash(raw []byte) string {
	digest := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
