package main

import (
	"strings"
	"testing"
)

func TestSubUsername(t *testing.T) {
	for _, ok := range []string{"bot01", "mm_desk", "abc"} {
		if !validSubUsername(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "ab", "_abc", "Abc", "a-b-c", "abcdefghijklmnopqrstu", "a@b"} {
		if validSubUsername(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if e := subLoginEmail("U123", "bot01"); !strings.HasSuffix(e, "@sub.linbit.invalid") || !strings.Contains(e, "bot01") {
		t.Fatalf("email %q", e)
	}
}

func TestNormalizePerms(t *testing.T) {
	p, err := normalizePerms([]string{"spot_trade"})
	if err != nil || strings.Join(p, ",") != "read,spot_trade" {
		t.Fatalf("got %v %v", p, err)
	}
	if _, err := normalizePerms([]string{"read", "withdraw"}); err == nil {
		t.Fatal("withdraw must be rejected")
	}
	if _, err := normalizePerms([]string{"transfer"}); err == nil {
		t.Fatal("unknown perm must be rejected")
	}
}

func TestIPAllowlist(t *testing.T) {
	l, err := normalizeIPs([]string{"1.2.3.4", "10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	if !ipAllowed(l, "1.2.3.4") || !ipAllowed(l, "10.9.9.9") || ipAllowed(l, "8.8.8.8") {
		t.Fatal("allowlist mismatch")
	}
	if !ipAllowed(nil, "8.8.8.8") {
		t.Fatal("empty allowlist allows all")
	}
	if _, err := normalizeIPs([]string{"not-an-ip"}); err == nil {
		t.Fatal("bad ip accepted")
	}
}
