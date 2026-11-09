package main

import (
	"testing"
	"time"
)

func TestParseHistoryRange(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	from, to, err := parseHistoryRange("", "", now)
	if err != nil || !to.Equal(now) || !from.Equal(now.AddDate(0, 0, -30)) {
		t.Fatalf("%s %s %v", from, to, err)
	}
	from, to, err = parseHistoryRange("2026-11-01", "2026-11-02", now)
	if err != nil || !from.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("inclusive end %s %s", from, to)
	}
	if _, _, err := parseHistoryRange("2020-01-01", "2026-01-01", now); err == nil {
		t.Fatal("span")
	}
}
