package auth

import (
	"errors"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

type Claims struct {
	Subject string
	Email   string
}

func FromRequest(request events.APIGatewayV2HTTPRequest) (Claims, error) {
	claims := request.RequestContext.Authorizer.JWT.Claims
	subject := strings.TrimSpace(claims["sub"])
	if subject == "" {
		return Claims{}, errors.New("missing authenticated subject")
	}
	return Claims{Subject: subject, Email: strings.TrimSpace(claims["email"])}, nil
}
