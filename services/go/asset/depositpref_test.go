package main

import "testing"

func TestResolveDepositAccount(t *testing.T) {
	if a, src := resolveDepositAccount("", "funding"); a != "funding" || src != "platform" {
		t.Fatalf("%s %s", a, src)
	}
	if a, src := resolveDepositAccount("spot", "funding"); a != "spot" || src != "user" {
		t.Fatalf("%s %s", a, src)
	}
	if a, src := resolveDepositAccount("nope", "nope"); a != "funding" || src != "platform" {
		t.Fatalf("%s %s", a, src)
	}
}
