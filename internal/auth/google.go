package auth

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/api/idtoken"
)

func VerifyGoogle(ctx context.Context, credential, audience string) (Claims, error) {
	credential, audience = strings.TrimSpace(credential), strings.TrimSpace(audience)
	if credential == "" || audience == "" {
		return Claims{}, ErrUnauthorized
	}
	payload, err := idtoken.Validate(ctx, credential, audience)
	if err != nil {
		return Claims{}, err
	}
	if payload.Issuer != "https://accounts.google.com" && payload.Issuer != "accounts.google.com" {
		return Claims{}, errors.New("invalid Google token issuer")
	}
	subject := strings.TrimSpace(payload.Subject)
	email, _ := payload.Claims["email"].(string)
	if subject == "" {
		return Claims{}, errors.New("missing Google token subject")
	}
	return Claims{Subject: subject, Email: strings.TrimSpace(email)}, nil
}
