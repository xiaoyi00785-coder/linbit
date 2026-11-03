package main

import (
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

type Collateral struct {
	Asset string          `json:"asset"`
	Cap   decimal.Decimal `json:"cap"` // per-user collateral cap
}

type LoanRules struct {
	Asset        string                     `json:"asset"`
	Rates        map[string]decimal.Decimal `json:"hourlyRates"` // term -> hourly rate
	TermDays     map[string]int             `json:"termDays"`
	UserLimit    decimal.Decimal            `json:"userLimit"`
	MinAmount    decimal.Decimal            `json:"minAmount"`
	InitialLTV   decimal.Decimal            `json:"initialLtv"`
	WarnLTV      decimal.Decimal            `json:"warnLtv"`
	LiqLTV       decimal.Decimal            `json:"liquidationLtv"`
	Collaterals  []Collateral               `json:"collaterals"`
	OverdueMult  decimal.Decimal            `json:"overdueMultiplier"`
	OverdueDays  int                        `json:"overdueLiquidationDays"`
	LiqFeeRate   decimal.Decimal            `json:"liquidationFeeRate"`
	EarlyPenalty decimal.Decimal            `json:"earlyRepayPenaltyRate"`
}

type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	DevMode       bool
	AssetURL      string
	FuturesURL    string
	TickEvery     time.Duration // EARN_TICK_MS (60000)
	Loan          LoanRules
}

func dec(key, def string) decimal.Decimal {
	d, err := decimal.NewFromString(common.Env(key, def))
	if err != nil {
		return decimal.RequireFromString(def)
	}
	return d
}

func parseCollaterals(s string) []Collateral {
	var out []Collateral
	for _, p := range strings.Split(s, ",") {
		f := strings.Split(strings.TrimSpace(p), ":")
		if len(f) != 2 {
			continue
		}
		c, err := decimal.NewFromString(f[1])
		if err != nil {
			continue
		}
		out = append(out, Collateral{Asset: strings.ToUpper(f[0]), Cap: c})
	}
	return out
}

func LoadConfig() Config {
	cols := parseCollaterals(common.Env("LOAN_COLLATERALS", "BTC:10,ETH:50"))
	if len(cols) == 0 {
		cols = parseCollaterals("BTC:10,ETH:50")
	}
	return Config{
		Port:          common.Env("PORT", "8092"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		DevMode:       common.Env("DEV_MODE", "") == "true",
		AssetURL:      common.Env("ASSET_INTERNAL_URL", "http://localhost:8082"),
		FuturesURL:    common.Env("FUTURES_INTERNAL_URL", "http://localhost:8089"),
		TickEvery:     time.Duration(common.EnvInt("EARN_TICK_MS", 60000)) * time.Millisecond,
		Loan: LoanRules{
			Asset: strings.ToUpper(common.Env("LOAN_ASSET", "USDT")),
			Rates: map[string]decimal.Decimal{
				"flexible": dec("LOAN_RATE_FLEXIBLE", "0.000003847"),
				"7d":       dec("LOAN_RATE_7D", "0.0000042808"),
				"30d":      dec("LOAN_RATE_30D", "0.0000068493"),
			},
			TermDays:     map[string]int{"flexible": 0, "7d": 7, "30d": 30},
			UserLimit:    dec("LOAN_USER_LIMIT", "1200000"),
			MinAmount:    dec("LOAN_MIN_AMOUNT", "10"),
			InitialLTV:   dec("LOAN_INITIAL_LTV", "0.80"),
			WarnLTV:      dec("LOAN_WARN_LTV", "0.88"),
			LiqLTV:       dec("LOAN_LIQUIDATION_LTV", "0.95"),
			Collaterals:  cols,
			OverdueMult:  dec("LOAN_OVERDUE_RATE_MULTIPLIER", "3"),
			OverdueDays:  common.EnvInt("LOAN_OVERDUE_LIQUIDATION_DAYS", 7),
			LiqFeeRate:   dec("LOAN_LIQUIDATION_FEE_RATE", "0.03"),
			EarlyPenalty: dec("LOAN_EARLY_REPAY_PENALTY_RATE", "0.0005"),
		},
	}
}
