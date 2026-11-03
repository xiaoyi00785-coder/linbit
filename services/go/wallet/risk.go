package main

import "github.com/shopspring/decimal"

// risk.go is the pluggable risk layer. A RiskScorer assigns a 0–100 risk score to
// a withdrawal; the score plus the amount decides whether the withdrawal is
// auto-released or held for manual review (see decide).

// RiskInput carries the signals the scorer looks at.
type RiskInput struct {
	Amount       decimal.Decimal
	NewAddress   bool // destination never used by this user before
	NewDevice    bool // request came from an unrecognised device
	WhitelistOn  bool // the user has the withdrawal whitelist enabled
	Count24h     int  // withdrawals submitted by this user in the last 24h
	AmountUSD24h decimal.Decimal
}

// RiskScorer scores a withdrawal. Implementations must be side-effect free.
type RiskScorer interface {
	Score(in RiskInput) int
}

// LocalRiskScorer is a simple rule-based scorer. The weights are not in the rules
// file and are configurable defaults (see summary); the thresholds that consume
// the score live in Config.Risk.
type LocalRiskScorer struct{}

// NewLocalRiskScorer returns the default rule-based scorer.
func NewLocalRiskScorer() *LocalRiskScorer { return &LocalRiskScorer{} }

// Score returns a 0–100 risk score. Higher is riskier.
func (LocalRiskScorer) Score(in RiskInput) int {
	score := 0
	if in.NewAddress {
		score += 10
	}
	if in.NewDevice {
		score += 10
	}
	// Frequent withdrawals in the last 24h ramp the score up.
	switch {
	case in.Count24h >= 10:
		score += 30
	case in.Count24h >= 5:
		score += 15
	case in.Count24h >= 3:
		score += 5
	}
	// A whitelisted destination (when the whitelist is enabled) is lower risk.
	if in.WhitelistOn {
		score -= 10
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// reviewDecision is the outcome of the auto-release / manual-review gate.
type reviewDecision int

const (
	decisionAuto   reviewDecision = iota // auto-release: sign & broadcast
	decisionReview                       // hold for manual admin review
)

// decide applies the thresholds from the rules file:
//   - amount ≤ AutoMaxAmount AND score ≤ AutoMaxScore            → auto-release
//   - amount ≥ ReviewMinAmount OR score > ReviewMaxScore         → manual review
//   - otherwise (the in-between band the rules leave blank)      → MiddleAction
func decide(t RiskThresholds, amount decimal.Decimal, score int) reviewDecision {
	if amount.LessThanOrEqual(t.AutoMaxAmount) && score <= t.AutoMaxScore {
		return decisionAuto
	}
	if amount.GreaterThanOrEqual(t.ReviewMinAmount) || score > t.ReviewMaxScore {
		return decisionReview
	}
	if t.MiddleAction == "auto" {
		return decisionAuto
	}
	return decisionReview
}
