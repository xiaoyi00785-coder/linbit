package main

// config.go：资产服务运行时配置。金额相关的默认币种和闪兑参数见各自文件。

import (
	"github.com/linbit/platform/services/go/common"
)

// Config 是资产服务的运行时配置。
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	RedisURL      string
	MarketURL     string // MARKET_URL: index prices (convert + USDT valuation)
	OrderURL      string // order service (convert hedge IOC orders)
	FuturesURL    string // FUTURES_INTERNAL_URL: futures wallet for /assets/overview
	Convert       ConvertDefaults
}

// LoadConfig 从环境变量组装 Config。端口默认 8082。
func LoadConfig() Config {
	return Config{
		Port:          common.Env("PORT", "8082"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		RedisURL:      common.Env("REDIS_URL", ""),
		MarketURL:     common.Env("MARKET_URL", "http://localhost:8085"),
		OrderURL:      common.Env("ORDER_URL", "http://localhost:8084"),
		FuturesURL:    common.Env("FUTURES_INTERNAL_URL", "http://localhost:8089"),
		Convert:       loadConvertDefaults(),
	}
}

// defaultAssets 在开户时种成零余额，GET /assets/balances（现货和资金账户）始终列出，
// 用户还没有持仓也看得到。和 convert_assets 种子（00005_convert.sql）以及钱包充值币种保持一致。
// USDC 只从借贷（earn）拿掉，现货里仍然是普通资产。
var defaultAssets = []string{"USDT", "USDC", "BTC", "ETH", "SOL", "BNB", "TRX"}
