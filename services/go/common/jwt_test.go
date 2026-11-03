package common

import (
	"testing"
	"time"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	secret := "test-secret"
	now := time.Now()
	tok, err := SignAccessToken(secret, "80000001", "trader@example.com", time.Hour, now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := ParseAccessToken(secret, tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Subject != "80000001" {
		t.Errorf("subject = %q, want 80000001", claims.Subject)
	}
	if claims.Email != "trader@example.com" {
		t.Errorf("email = %q", claims.Email)
	}
}

func TestParseRejectsWrongSecret(t *testing.T) {
	now := time.Now()
	tok, _ := SignAccessToken("secret-a", "80000001", "a@b.c", time.Hour, now)
	if _, err := ParseAccessToken("secret-b", tok); err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	secret := "s"
	past := time.Now().Add(-2 * time.Hour)
	tok, _ := SignAccessToken(secret, "80000001", "a@b.c", time.Hour, past)
	if _, err := ParseAccessToken(secret, tok); err == nil {
		t.Fatal("expected error for expired token")
	}
}
