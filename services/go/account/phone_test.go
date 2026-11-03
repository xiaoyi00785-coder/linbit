package main

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		phone, cc    string
		wantNorm     string
		wantDial     string
		wantOK       bool
	}{
		{"+8613800138000", "", "+8613800138000", "", true},   // already E.164
		{"13800138000", "86", "+8613800138000", "86", true},  // national + dial code
		{"138-0013-8000", "86", "+8613800138000", "86", true}, // punctuation stripped
		{"+1 650 1", "", "", "", false},                      // space inside E.164 -> invalid
		{"13800138000", "", "", "", false},                   // no dial code
		{"13800138000", "+86", "+8613800138000", "86", true}, // dial code may carry '+'
		{"", "86", "", "", false},                            // empty national part
		{"abc", "86", "", "", false},                         // non-numeric
		{"+0123", "", "", "", false},                         // E.164 cannot start with 0
	}
	for _, c := range cases {
		norm, dial, ok := normalizePhone(c.phone, c.cc)
		if ok != c.wantOK || norm != c.wantNorm || dial != c.wantDial {
			t.Errorf("normalizePhone(%q,%q) = (%q,%q,%v), want (%q,%q,%v)",
				c.phone, c.cc, norm, dial, ok, c.wantNorm, c.wantDial, c.wantOK)
		}
	}
}

func TestValidEmail(t *testing.T) {
	ok := []string{"a@b.com", "trader+1@example.co.uk"}
	bad := []string{"", "no-at", "a@b", "a@@b.com", "@b.com", "a@.com", "a@b."}
	for _, e := range ok {
		if !validEmail(e) {
			t.Errorf("validEmail(%q) = false, want true", e)
		}
	}
	for _, e := range bad {
		if validEmail(e) {
			t.Errorf("validEmail(%q) = true, want false", e)
		}
	}
}

func TestValidAvatarURL(t *testing.T) {
	if !validAvatarURL("https://cdn.example.com/a.png") {
		t.Error("valid https avatar rejected")
	}
	if validAvatarURL("ftp://x/a.png") || validAvatarURL("not a url") || validAvatarURL("/local/a.png") {
		t.Error("invalid avatar accepted")
	}
}

func TestGenerateNumericCode(t *testing.T) {
	c, err := generateNumericCode(6)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if len(c) != 6 {
		t.Fatalf("len = %d, want 6 (code=%q)", len(c), c)
	}
	for _, r := range c {
		if r < '0' || r > '9' {
			t.Fatalf("non-digit in code %q", c)
		}
	}
}

func TestResolveIdentity(t *testing.T) {
	if id, ok := resolveIdentity("Trader@Example.com", "", ""); !ok || id.channel != channelEmail || id.target != "trader@example.com" {
		t.Errorf("email identity = %+v ok=%v", id, ok)
	}
	if id, ok := resolveIdentity("", "13800138000", "86"); !ok || id.channel != channelSMS || id.target != "+8613800138000" {
		t.Errorf("phone identity = %+v ok=%v", id, ok)
	}
	if _, ok := resolveIdentity("", "", ""); ok {
		t.Error("empty identity accepted")
	}
}
