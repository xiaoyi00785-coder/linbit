package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func defaultThresholds() RiskThresholds {
	return RiskThresholds{
		AutoMaxAmount:   d("1000"),
		AutoMaxScore:    20,
		ReviewMinAmount: d("5000"),
		ReviewMaxScore:  40,
		MiddleAction:    "review",
	}
}

// TestDecideBoundaries pins the rules-file thresholds:
//
//	≤1000U and score ≤20 → auto; ≥5000U or score >40 → review;
//	in-between → MiddleAction (default review).
func TestDecideBoundaries(t *testing.T) {
	th := defaultThresholds()
	cases := []struct {
		name   string
		amount string
		score  int
		want   reviewDecision
	}{
		{"small & low score auto", "100", 0, decisionAuto},
		{"exactly 1000 and score 20 auto", "1000", 20, decisionAuto},
		{"above 1000 but below 5000 -> middle review", "1000.00000001", 20, decisionReview},
		{"score 21 blocks auto, middle -> review", "1000", 21, decisionReview},
		{"5000 forces review", "5000", 0, decisionReview},
		{"score 41 forces review", "100", 41, decisionReview},
		{"score 40 mid-range still review by middle", "2000", 40, decisionReview},
		{"small amount high score review", "50", 60, decisionReview},
	}
	for _, c := range cases {
		got := decide(th, d(c.amount), c.score)
		if got != c.want {
			t.Errorf("%s: decide(%s, %d) = %v, want %v", c.name, c.amount, c.score, got, c.want)
		}
	}
}

// TestDecideMiddleAuto checks the configurable in-between band can be set to
// auto-release.
func TestDecideMiddleAuto(t *testing.T) {
	th := defaultThresholds()
	th.MiddleAction = "auto"
	if got := decide(th, d("2000"), 25); got != decisionAuto {
		t.Fatalf("middle auto: got %v, want auto", got)
	}
	// The ≥5000 hard review still wins over the middle-auto setting.
	if got := decide(th, d("6000"), 0); got != decisionReview {
		t.Fatalf(">=5000 should still review, got %v", got)
	}
}

func TestLocalRiskScorer(t *testing.T) {
	s := NewLocalRiskScorer()
	// Baseline: known address, known device, low frequency → 0.
	if got := s.Score(RiskInput{Amount: d("100")}); got != 0 {
		t.Fatalf("baseline score = %d, want 0", got)
	}
	// New address (+10) and new device (+10) = 20 (the auto ceiling).
	if got := s.Score(RiskInput{NewAddress: true, NewDevice: true}); got != 20 {
		t.Fatalf("new addr+device = %d, want 20", got)
	}
	// Whitelisted destination reduces by 10 (floor 0).
	if got := s.Score(RiskInput{WhitelistOn: true}); got != 0 {
		t.Fatalf("whitelist-only = %d, want 0", got)
	}
	// High frequency saturates.
	if got := s.Score(RiskInput{Count24h: 12}); got != 30 {
		t.Fatalf("count24h=12 = %d, want 30", got)
	}
}
