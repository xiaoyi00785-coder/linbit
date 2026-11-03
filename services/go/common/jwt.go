package common

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken is returned when an access token fails validation.
var ErrInvalidToken = errors.New("invalid token")

// AccessClaims are the custom claims embedded in a Linbit access token.
// Subject holds the public user id (uid).
type AccessClaims struct {
	Email string `json:"email"`
	Typ   string `json:"typ"`
	// Acct is "sub" for a sub-account session (empty for a normal account);
	// Parent is then the parent's uid. The gateway uses it to fence sub-account
	// sessions off parent-only and withdrawal/deposit endpoints.
	Acct   string `json:"acct,omitempty"`
	Parent string `json:"parent,omitempty"`
	jwt.RegisteredClaims
}

// SignAccessToken issues an HS256 access token for the given uid/email.
func SignAccessToken(secret, uid, email string, ttl time.Duration, now time.Time) (string, error) {
	return SignAccessTokenFor(secret, uid, email, "", ttl, now)
}

// SignAccessTokenFor issues an access token; a non-empty parentUID marks the
// session as a sub-account session (acct="sub").
func SignAccessTokenFor(secret, uid, email, parentUID string, ttl time.Duration, now time.Time) (string, error) {
	acct := ""
	if parentUID != "" {
		acct = "sub"
	}
	claims := AccessClaims{
		Email:  email,
		Typ:    "access",
		Acct:   acct,
		Parent: parentUID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uid,
			Issuer:    "linbit",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}

// ParseAccessToken validates an HS256 access token and returns its claims.
func ParseAccessToken(secret, token string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidToken, t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !parsed.Valid {
		return nil, ErrInvalidToken
	}
	if claims.Typ != "access" {
		return nil, fmt.Errorf("%w: wrong token type", ErrInvalidToken)
	}
	return claims, nil
}
