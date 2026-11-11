package main

import "github.com/shopspring/decimal"

// risk.go：提现风控。RiskScorer 打 0–100 分，分数加金额决定自动放行还是人工审核（见 decide）。
// 权重不在规则文件里，是代码里的默认值；阈值在 Config.Risk。

// RiskInput 是打分用的信号。金额本身不进分数，只在 decide 里和阈值比。
type RiskInput struct {
	Amount       decimal.Decimal
	NewAddress   bool // destination never used by this user before
	NewDevice    bool // request came from an unrecognised device
	WhitelistOn  bool // the user has the withdrawal whitelist enabled
	Count24h     int  // withdrawals submitted by this user in the last 24h
	AmountUSD24h decimal.Decimal
}

// RiskScorer 给一笔提现打分。实现不得有副作用。
type RiskScorer interface {
	Score(in RiskInput) int
}

// LocalRiskScorer 是规则打分：新地址 +10，新设备 +10，
// 24h 笔数 ≥3/5/10 分别 +5/+15/+30，开了提现白名单 -10，结果夹在 0–100。
type LocalRiskScorer struct{}

// NewLocalRiskScorer 返回上面这套默认打分。
func NewLocalRiskScorer() *LocalRiskScorer { return &LocalRiskScorer{} }

// Score 返回 0–100，越高越险。
func (LocalRiskScorer) Score(in RiskInput) int {
	score := 0
	if in.NewAddress {
		score += 10
	}
	if in.NewDevice {
		score += 10
	}
	// 近 24h 提现越密，分越高。
	switch {
	case in.Count24h >= 10:
		score += 30
	case in.Count24h >= 5:
		score += 15
	case in.Count24h >= 3:
		score += 5
	}
	// 开了提现白名单再往外打，风险低一档。
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

// reviewDecision 是自动放行还是转人工。
type reviewDecision int

const (
	decisionAuto   reviewDecision = iota // auto-release: sign & broadcast
	decisionReview                       // hold for manual admin review
)

// decide 用 Config.Risk 的阈值（金额应为 USDT 等值，调用方负责折算）：
//   - amount ≤ AutoMaxAmount 且 score ≤ AutoMaxScore     → 自动放行
//   - amount ≥ ReviewMinAmount 或 score > ReviewMaxScore → 人工审核
//   - 中间带（规则留空）                                  → MiddleAction
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
