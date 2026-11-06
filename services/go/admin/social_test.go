package main

import (
	"strings"
	"testing"
)

func TestSocialURLValidation(t *testing.T) {
	valid := map[string]string{
		"  https://t.me/linbit  ":     "https://t.me/linbit",
		"https://discord.gg/a?x=1":    "https://discord.gg/a?x=1",
		"http://localhost:3000/group": "http://localhost:3000/group",
	}
	for raw, want := range valid {
		got, err := socialURL(raw)
		if err != nil || got != want {
			t.Errorf("socialURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "https://example.com\nSet-Cookie:x=y", "//t.me/no-protocol", "https://", "ftp://example.com/a", "https://user:password@example.com/", "https://example.com/" + strings.Repeat("x", 2048)} {
		if _, err := socialURL(raw); err == nil {
			t.Errorf("socialURL accepted unsafe URL %q", raw)
		}
	}
}

func TestSocialPatchAndPublicFiltering(t *testing.T) {
	x, telegram := "https://x.com/linbit", ""
	patch, err := validateSocialPatch(map[string]*string{"x": &x, "telegram": &telegram})
	if err != nil || patch["x"] != x || patch["telegram"] != "" {
		t.Fatalf("valid patch: %#v, %v", patch, err)
	}
	unknown := "https://example.com"
	if _, err := validateSocialPatch(map[string]*string{"unknown": &unknown}); err == nil {
		t.Fatal("unknown channel was accepted")
	}
	if _, err := validateSocialPatch(map[string]*string{"x": nil}); err == nil {
		t.Fatal("null URL was accepted")
	}
	public := publicSocialLinks(map[string]string{
		"x": "https://x.com/linbit", "telegram": "http://t.me/linbit", "youtube": "javascript:alert(1)", "unknown": "https://example.com",
	})
	if public["x"] != "https://x.com/linbit" || public["telegram"] != "" || public["youtube"] != "" || len(public) != len(socialChannels) {
		t.Fatalf("unsafe public result: %#v", public)
	}
	if _, ok := public["unknown"]; ok {
		t.Fatal("unknown channel leaked")
	}
}
