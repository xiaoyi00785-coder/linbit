package main

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// hashPassword derives a bcrypt hash for the plaintext password.
func hashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// checkPassword reports whether plain matches the stored bcrypt hash.
func checkPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// newRefreshToken returns a cryptographically random opaque refresh token.
func newRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// generateTOTP creates a new TOTP secret and otpauth URI for the account email.
func generateTOTP(email string) (secret, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Linbit", AccountName: email})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// validateTOTP checks a 6-digit code against the secret.
func validateTOTP(secret, code string) bool {
	return totp.Validate(code, secret)
}
