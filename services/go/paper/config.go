package main

// 模拟盘配置。指数最大年龄、预警写限流必须为正，main 启动时会拒绝非正值。合约行情地址是 FUTURES_URL。

import (
	"github.com/linbit/platform/services/go/common"
	"strings"
	"time"
)

// Config 的 PaperTradingEnabled / PriceAlertsEnabled 非空时压过库里的功能开关。
type Config struct {
	Port                string
	DatabaseURL         string
	InternalToken       string
	JWTSecret           string
	MarketURL           string
	AccountURL          string
	IndexMaxAge         time.Duration
	EvaluationInterval  time.Duration
	DeliveryInterval    time.Duration
	AlertCooldown       time.Duration
	AlertWriteLimit     int
	AlertWriteWindow    time.Duration
	DevMode             bool
	PaperTradingEnabled string
	PriceAlertsEnabled  string
	FuturesURL          string
}

// LoadConfig 从环境变量读配置。DATABASE_URL、INTERNAL_TOKEN、JWT_SECRET 缺失会在 MustEnv 处失败。默认端口 8094。
func LoadConfig() Config {
	return Config{
		Port: common.Env("PORT", "8094"), DatabaseURL: common.MustEnv("DATABASE_URL"),
		InternalToken: common.MustEnv("INTERNAL_TOKEN"), JWTSecret: common.MustEnv("JWT_SECRET"),
		MarketURL: common.Env("MARKET_URL", "http://localhost:8085"), AccountURL: common.Env("ACCOUNT_INTERNAL_URL", "http://localhost:8081"),
		IndexMaxAge:         time.Duration(common.EnvInt("PAPER_INDEX_MAX_AGE_SECONDS", 10)) * time.Second,
		EvaluationInterval:  time.Duration(common.EnvInt("PRICE_ALERT_EVALUATION_MS", 2000)) * time.Millisecond,
		DeliveryInterval:    time.Duration(common.EnvInt("PRICE_ALERT_DELIVERY_MS", 1000)) * time.Millisecond,
		AlertCooldown:       time.Duration(common.EnvInt("USER_PRICE_ALERT_COOLDOWN_SECONDS", 300)) * time.Second,
		AlertWriteLimit:     common.EnvInt("PRICE_ALERT_WRITE_LIMIT", 20),
		AlertWriteWindow:    time.Duration(common.EnvInt("PRICE_ALERT_WRITE_WINDOW_SECONDS", 60)) * time.Second,
		DevMode:             common.Env("DEV_MODE", "false") == "true",
		PaperTradingEnabled: strings.TrimSpace(common.Env("PAPER_TRADING_ENABLED", "")),
		PriceAlertsEnabled:  strings.TrimSpace(common.Env("PRICE_ALERTS_ENABLED", "")),
		FuturesURL:          strings.TrimRight(common.Env("FUTURES_URL", "http://localhost:8089"), "/"),
	}
}
