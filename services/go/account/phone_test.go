package main

import (
	"errors"
	"testing"
)

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		phone, cc string
		wantNorm  string
		wantDial  string
		wantErr   error // nil = success; otherwise the specific sentinel expected
	}{
		{"+8613800138000", "", "+8613800138000", "", nil},    // already E.164
		{"13800138000", "86", "+8613800138000", "86", nil},   // national + dial code
		{"138-0013-8000", "86", "+8613800138000", "86", nil}, // punctuation stripped
		{"+1 650 1", "", "", "", ErrPhoneFormat},             // space inside E.164 -> invalid
		{"13800138000", "", "", "", ErrPhoneCountryCode},     // no dial code
		{"13800138000", "+86", "+8613800138000", "86", nil},  // dial code may carry '+'
		{"", "86", "", "", ErrPhoneFormat},                   // empty national part
		{"abc", "86", "", "", ErrPhoneFormat},                // non-numeric
		{"+0123", "", "", "", ErrPhoneFormat},                // E.164 cannot start with 0
	}
	for _, c := range cases {
		norm, dial, err := normalizePhone(c.phone, c.cc)
		if !errors.Is(err, c.wantErr) || norm != c.wantNorm || dial != c.wantDial {
			t.Errorf("normalizePhone(%q,%q) = (%q,%q,%v), want (%q,%q,%v)",
				c.phone, c.cc, norm, dial, err, c.wantNorm, c.wantDial, c.wantErr)
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
	if id, err := resolveIdentity("Trader@Example.com", "", ""); err != nil || id.channel != channelEmail || id.target != "trader@example.com" {
		t.Errorf("email identity = %+v err=%v", id, err)
	}
	if id, err := resolveIdentity("", "13800138000", "86"); err != nil || id.channel != channelSMS || id.target != "+8613800138000" {
		t.Errorf("phone identity = %+v err=%v", id, err)
	}
	// Specific errors propagate so callers can give a precise 400 message.
	if _, err := resolveIdentity("", "", ""); !errors.Is(err, errNoIdentity) {
		t.Errorf("empty identity err = %v, want errNoIdentity", err)
	}
	if _, err := resolveIdentity("", "13800138000", ""); !errors.Is(err, ErrPhoneCountryCode) {
		t.Errorf("phone without country code err = %v, want ErrPhoneCountryCode", err)
	}
	if _, err := resolveIdentity("not-an-email", "", ""); !errors.Is(err, errBadEmail) {
		t.Errorf("bad email err = %v, want errBadEmail", err)
	}
}
