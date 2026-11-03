package main

import "testing"

func TestComputeSecurityLevel(t *testing.T) {
	l := computeSecurityLevel(true, false, false, false, false)
	if l.Score != 1 || l.Max != 5 || l.Level != "low" || len(l.Missing) != 4 {
		t.Fatalf("got %+v", l)
	}
	l = computeSecurityLevel(true, true, true, false, false)
	if l.Score != 3 || l.Level != "medium" {
		t.Fatalf("got %+v", l)
	}
	l = computeSecurityLevel(true, true, true, true, true)
	if l.Score != 5 || l.Level != "high" || len(l.Missing) != 0 {
		t.Fatalf("got %+v", l)
	}
}

func TestMaskEmailMatchesWeb(t *testing.T) {
	if got := maskEmail("xiaoyi@example.com"); got != "xia***@example.com" {
		t.Fatalf("got %q", got)
	}
}
