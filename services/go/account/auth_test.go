package main

import "testing"

func TestPasswordHashing(t *testing.T) {
	hash, err := hashPassword("Str0ng-Passw0rd!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !checkPassword(hash, "Str0ng-Passw0rd!") {
		t.Error("correct password rejected")
	}
	if checkPassword(hash, "wrong") {
		t.Error("wrong password accepted")
	}
}

func TestTOTPGenerateAndValidate(t *testing.T) {
	secret, uri, err := generateTOTP("trader@example.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if secret == "" || uri == "" {
		t.Fatal("empty secret/uri")
	}
	if validateTOTP(secret, "000000") && validateTOTP(secret, "111111") {
		t.Error("totp accepted obviously invalid codes")
	}
}
