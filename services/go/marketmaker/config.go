package main

import (
	"os"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Config is the marketmaker runtime configuration.
type Config struct {
	Port               string
	DatabaseURL        string
	InternalToken      string
	OrderURL           string
	FuturesURL         string
	MarketURL          string
	AssetURL           string
	Enabled            bool          // MM_ENABLED: master switch; mainnet default false, testnet default true
	UserID             string        // the market-maker account (MM_USER_ID)
	Tick               time.Duration // scheduler granularity; each pair refreshes on its own refresh_ms
	SeedAmount         string        // virtual USDT per mm_testnet_seed (testnet/mock only)
	SeedBaseUSDT       string        // USDT value per base-asset seed (MM_SEED_BASE_USDT)
	SeedBaseAssets     string        // base assets allowed in a seed set (MM_SEED_BASE_ASSETS)
	TakerEnabled       bool          // MM_TAKER_ENABLED: second account takes MM quotes so the book prints trades
	TakerUserID        string        // MM_TAKER_USER_ID (must differ from MM_USER_ID; STP blocks self-trade)
	TakerNotional      decimal.Decimal
	TakerInterval      time.Duration
	TakerLightInterval time.Duration   // MM_TAKER_LIGHT_INTERVAL_MS: taker cadence for light-profile (non book-following) pairs
	BookFeed           string          // MM_BOOK_FEED: binance,okx,bybit
	BookScale          decimal.Decimal // MM_BOOK_SCALE: multiply copied qty (default 1)
	BookTTL            time.Duration   // MM_BOOK_TTL_MS cache of one venue snapshot
	// MM_BOOK_FOLLOW: when set, only these pairs copy the external book
	// (SYMBOL for both markets, or spot:SYMBOL / futures:SYMBOL); every
	// other pair quotes the light index ladder, which needs no venue REST
	// depth call. Empty = every pair follows the book (old behaviour).
	BookFollow       map[string]bool
	FuturesAmend     bool            // MM_FUTURES_AMEND: amend futures quotes in place (default true)
	LightNotionalMax decimal.Decimal // MM_LIGHT_NOTIONAL_MAX: per-level USDT cap for light-profile pairs
	BinanceURL       string
	BinanceFAPIURL   string
	OKXURL           string
	BybitURL         string

	Workers         int // MM_WORKERS: pairs refreshed concurrently
	PairConcurrency int // MM_PAIR_CONCURRENCY: order calls in flight per pair
}

// LoadConfig reads the environment.
func LoadConfig() Config {
	return Config{
		Port:               common.Env("PORT", "8093"),
		DatabaseURL:        common.MustEnv("DATABASE_URL"),
		InternalToken:      common.Env("INTERNAL_TOKEN", ""),
		OrderURL:           common.Env("ORDER_URL", "http://localhost:8084"),
		FuturesURL:         common.Env("FUTURES_URL", "http://localhost:8089"),
		MarketURL:          common.Env("MARKET_URL", "http://localhost:8085"),
		AssetURL:           common.Env("ASSET_URL", "http://localhost:8082"),
		Enabled:            mmEnabled(),
		UserID:             common.Env("MM_USER_ID", "mm:main"),
		Tick:               time.Duration(common.EnvInt("MM_TICK_MS", 500)) * time.Millisecond,
		SeedAmount:         common.Env("MM_SEED_AMOUNT", "10000000"),
		SeedBaseUSDT:       common.Env("MM_SEED_BASE_USDT", "1000000"),
		SeedBaseAssets:     common.Env("MM_SEED_BASE_ASSETS", "BTC,ETH,SOL,BNB,TRX,AAPL,TSLA,NVDA,MSFT,GOOGL,AMZN,META,COIN,SPY,PAXG,XAUT"),
		TakerEnabled:       mmTakerEnabled(),
		TakerUserID:        common.Env("MM_TAKER_USER_ID", "taker:mm"),
		TakerNotional:      decEnv("MM_TAKER_NOTIONAL", "1000"),
		TakerInterval:      time.Duration(common.EnvInt("MM_TAKER_INTERVAL_MS", 800)) * time.Millisecond,
		TakerLightInterval: time.Duration(common.EnvInt("MM_TAKER_LIGHT_INTERVAL_MS", 15000)) * time.Millisecond,
		BookFeed:           common.Env("MM_BOOK_FEED", "binance,okx,bybit"),
		BookScale:          decEnv("MM_BOOK_SCALE", "1"),
		BookTTL:            time.Duration(common.EnvInt("MM_BOOK_TTL_MS", 1000)) * time.Millisecond,
		BookFollow:         bookFollowSet(common.Env("MM_BOOK_FOLLOW", "")),
		LightNotionalMax:   decEnv("MM_LIGHT_NOTIONAL_MAX", "5000"),
		FuturesAmend:       common.Env("MM_FUTURES_AMEND", "1") != "0",
		BinanceURL:         common.Env("BINANCE_API_URL", "https://data-api.binance.vision"),
		BinanceFAPIURL:     common.Env("BINANCE_FAPI_URL", "https://fapi.binance.com"),
		OKXURL:             common.Env("OKX_API_URL", "https://www.okx.com"),
		BybitURL:           common.Env("BYBIT_API_URL", "https://api.bybit.com"),
		Workers:            max(1, common.EnvInt("MM_WORKERS", 8)),
		PairConcurrency:    max(1, common.EnvInt("MM_PAIR_CONCURRENCY", 4)),
	}
}

func decEnv(key, def string) decimal.Decimal {
	d, err := decimal.NewFromString(strings.TrimSpace(common.Env(key, def)))
	if err != nil || !d.IsPositive() {
		return decimal.RequireFromString(def)
	}
	return d
}

func mmTakerEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MM_TAKER_ENABLED")))
	if v == "" {
		return true
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// mmEnabled reads MM_ENABLED; unset defaults to false on mainnet (owner
// decision: market maker stopped at mainnet launch) and true elsewhere.
func mmEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MM_ENABLED")))
	if v == "" {
		return !common.IsMainnet()
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func bookFollowSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if i := strings.Index(f, ":"); i > 0 {
			out[strings.ToLower(f[:i])+":"+strings.ToUpper(f[i+1:])] = true
			continue
		}
		out[strings.ToUpper(f)] = true
	}
	return out
}

// followsBook reports whether a pair copies the external book.
func (c Config) followsBook(market, symbol string) bool {
	if len(c.BookFollow) == 0 {
		return true
	}
	sym := strings.ToUpper(symbol)
	return c.BookFollow[sym] || c.BookFollow[strings.ToLower(market)+":"+sym]
}
