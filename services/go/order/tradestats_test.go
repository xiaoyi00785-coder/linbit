package main

import (
	"testing"
	"time"
)

func TestParseSpotRange(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	from, to, err := parseSpotRange("", "", now)
	if err != nil || !from.Equal(now.AddDate(0, 0, -30)) || !to.Equal(now) {
		t.Fatalf("%s %s %v", from, to, err)
	}
	if _, _, err := parseSpotRange("nope", "", now); err == nil {
		t.Fatal("bad from")
	}
}
