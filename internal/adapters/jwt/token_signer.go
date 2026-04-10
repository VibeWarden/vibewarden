package jwt

import (
	"context"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
)

// devTokenClaims holds the custom claims written into the dev JWT.
type devTokenClaims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// SignToken signs a JWT with the dev RSA private key and returns the compact
// serialisation. It is a pure function (aside from time.Now()) and is the
// single source of truth for dev token generation shared by the HTTP handler
// and the CLI command.
func SignToken(ctx context.Context, kp *DevKeyPair, sub, email, name, role string, ttl time.Duration) (string, error) {
	return signDevToken(ctx, kp, sub, email, name, role, ttl)
}

// signDevToken is the internal implementation of SignToken.
func signDevToken(_ context.Context, kp *DevKeyPair, sub, email, name, role string, ttl time.Duration) (string, error) {
	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: kp.PrivateKey},
		(&jose.SignerOptions{}).
			WithType("JWT").
			WithHeader("kid", DevKID),
	)
	if err != nil {
		return "", fmt.Errorf("creating signer: %w", err)
	}

	now := time.Now()
	std := josejwt.Claims{
		Issuer:   DevIssuer,
		Audience: josejwt.Audience{DevAudience},
		Subject:  sub,
		IssuedAt: josejwt.NewNumericDate(now),
		Expiry:   josejwt.NewNumericDate(now.Add(ttl)),
	}

	custom := devTokenClaims{
		Email: email,
		Name:  name,
		Role:  role,
	}

	raw, err := josejwt.Signed(sig).Claims(std).Claims(custom).Serialize()
	if err != nil {
		return "", fmt.Errorf("serialising token: %w", err)
	}

	return raw, nil
}
