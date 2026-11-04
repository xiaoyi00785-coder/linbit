package main

import (
	"strings"
	"testing"
)

func TestValidLoginPassword(t *testing.T) {
	for p, want := range map[string]bool{
		"ab1": false, "abcdefgh": false, "12345678": false, "abc12345": true,
		"Passw0rd!": true, strings.Repeat("a", 63) + "1": true, strings.Repeat("a", 64) + "1": false,
		"密码密码密码12": false,
	} {
		if got := validLoginPassword(p); got != want {
			t.Errorf("validLoginPassword(%q)=%v want %v", p, got, want)
		}
	}
}
