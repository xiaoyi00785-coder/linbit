package main

import (
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Tier is one partner commission tier. Promotion is by the directs' futures
// volume over the last TierWindow days (business rules).
type Tier struct {
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	MinVolume   decimal.Decimal `json:"minVolume30d"`
	FuturesRate decimal.Decimal `json:"futuresRate"`
	SpotRate    decimal.Decimal `json:"spotRate"`
}

type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	DevMode       bool
	AccountURL    string
	AssetURL      string
	OrderURL      string
	FuturesURL    string

	SyncEvery     time.Duration   // GROWTH_SYNC_MS
	SettleHourUTC int             // GROWTH_SETTLE_HOUR_UTC (16 = 00:00 UTC+8)
	DayOffset     time.Duration   // GROWTH_DAY_UTC_OFFSET_HOURS (8): business day boundary
	PayoutAsset   string          // GROWTH_PAYOUT_ASSET (USDT)
	USDQuotes     map[string]bool // GROWTH_USD_QUOTES (USDT,USDC) — spot quotes valued 1:1
	InviteBaseURL string          // GROWTH_INVITE_BASE_URL

	Tiers          []Tier          // GROWTH_PARTNER_TIERS key:minVol:futRate:spotRate,...
	TierWindow     time.Duration   // GROWTH_TIER_WINDOW_DAYS (30)
	OverrideRate   decimal.Decimal // GROWTH_PARTNER_OVERRIDE_RATE (0.10)
	OverrideDepth  int             // GROWTH_PARTNER_OVERRIDE_DEPTH (1 => two levels total)
	PartnerAutoOK  bool            // GROWTH_PARTNER_AUTO_APPROVE (false)
	ReferralRate   decimal.Decimal // GROWTH_REFERRAL_RATE (0.10)
	ReferralPeriod time.Duration   // GROWTH_REFERRAL_DAYS (365)

	CouponActivateDays int             // GROWTH_COUPON_ACTIVATE_DAYS (30): issued voucher must be activated within
	CouponValidDays    int             // GROWTH_COUPON_VALID_DAYS (14): bonus lifetime after activation
	CouponOwnFunds     bool            // GROWTH_COUPON_REQUIRE_OWN_FUNDS (true): trial needs own futures funds >= amount
	ValidInviteeFees   decimal.Decimal // GROWTH_VALID_INVITEE_FEES (100): cumulative fees of a "valid" invitee
	Points             PointsRules     // GROWTH_POINTS_* defaults for new carnival periods
}

func dec(key, def string) decimal.Decimal {
	d, err := decimal.NewFromString(common.Env(key, def))
	if err != nil {
		return decimal.RequireFromString(def)
	}
	return d
}

var tierNames = map[string]string{"normal": "普通合伙人", "silver": "白银合伙人", "gold": "黄金合伙人", "diamond": "钻石合伙人"}

func parseTiers(s string) []Tier {
	var out []Tier
	for _, part := range strings.Split(s, ",") {
		f := strings.Split(strings.TrimSpace(part), ":")
		if len(f) != 4 {
			continue
		}
		mv, e1 := decimal.NewFromString(f[1])
		fr, e2 := decimal.NewFromString(f[2])
		sr, e3 := decimal.NewFromString(f[3])
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		name := tierNames[f[0]]
		if name == "" {
			name = f[0]
		}
		out = append(out, Tier{Key: f[0], Name: name, MinVolume: mv, FuturesRate: fr, SpotRate: sr})
	}
	return out
}

const defaultTiers = "normal:0:0.30:0.20,silver:5000000:0.40:0.25,gold:20000000:0.45:0.30,diamond:50000000:0.50:0.35"

func LoadConfig() Config {
	quotes := map[string]bool{}
	for _, q := range strings.Split(common.Env("GROWTH_USD_QUOTES", "USDT,USDC"), ",") {
		if q = strings.ToUpper(strings.TrimSpace(q)); q != "" {
			quotes[q] = true
		}
	}
	tiers := parseTiers(common.Env("GROWTH_PARTNER_TIERS", defaultTiers))
	if len(tiers) == 0 {
		tiers = parseTiers(defaultTiers)
	}
	return Config{
		Port:          common.Env("PORT", "8091"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		DevMode:       common.Env("DEV_MODE", "") == "true",
		AccountURL:    common.Env("ACCOUNT_INTERNAL_URL", "http://localhost:8081"),
		AssetURL:      common.Env("ASSET_INTERNAL_URL", "http://localhost:8082"),
		OrderURL:      common.Env("ORDER_INTERNAL_URL", "http://localhost:8084"),
		FuturesURL:    common.Env("FUTURES_INTERNAL_URL", "http://localhost:8089"),

		SyncEvery:     time.Duration(common.EnvInt("GROWTH_SYNC_MS", 2000)) * time.Millisecond,
		SettleHourUTC: common.EnvInt("GROWTH_SETTLE_HOUR_UTC", 16),
		DayOffset:     time.Duration(common.EnvInt("GROWTH_DAY_UTC_OFFSET_HOURS", 8)) * time.Hour,
		PayoutAsset:   strings.ToUpper(common.Env("GROWTH_PAYOUT_ASSET", "USDT")),
		USDQuotes:     quotes,
		InviteBaseURL: common.Env("GROWTH_INVITE_BASE_URL", "https://linbit.example.com/r/"),

		Tiers:          tiers,
		TierWindow:     time.Duration(common.EnvInt("GROWTH_TIER_WINDOW_DAYS", 30)) * 24 * time.Hour,
		OverrideRate:   dec("GROWTH_PARTNER_OVERRIDE_RATE", "0.10"),
		OverrideDepth:  common.EnvInt("GROWTH_PARTNER_OVERRIDE_DEPTH", 1),
		PartnerAutoOK:  common.Env("GROWTH_PARTNER_AUTO_APPROVE", "false") == "true",
		ReferralRate:   dec("GROWTH_REFERRAL_RATE", "0.10"),
		ReferralPeriod: time.Duration(common.EnvInt("GROWTH_REFERRAL_DAYS", 365)) * 24 * time.Hour,

		CouponActivateDays: common.EnvInt("GROWTH_COUPON_ACTIVATE_DAYS", 30),
		CouponValidDays:    common.EnvInt("GROWTH_COUPON_VALID_DAYS", 14),
		CouponOwnFunds:     common.Env("GROWTH_COUPON_REQUIRE_OWN_FUNDS", "true") == "true",
		ValidInviteeFees:   dec("GROWTH_VALID_INVITEE_FEES", "100"),
		Points:             loadPointsRules(),
	}
}

// tierFor returns the highest tier whose threshold the volume reaches.
func (c Config) tierFor(vol decimal.Decimal) Tier {
	t := c.Tiers[0]
	for _, x := range c.Tiers {
		if vol.GreaterThanOrEqual(x.MinVolume) {
			t = x
		}
	}
	return t
}

func (c Config) tier(key string) Tier {
	for _, x := range c.Tiers {
		if x.Key == key {
			return x
		}
	}
	return c.Tiers[0]
}
