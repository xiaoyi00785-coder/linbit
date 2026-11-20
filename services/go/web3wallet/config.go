package main

import (
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// RPC is the per-chain RPC configuration. Each chain has one required endpoint
// plus an optional list of fallbacks tried round-robin on error.
type RPC struct {
	Primary   string
	Fallbacks []string
}

func (r RPC) Endpoints() []string {
	out := make([]string, 0, 1+len(r.Fallbacks))
	if r.Primary != "" {
		out = append(out, r.Primary)
	}
	out = append(out, r.Fallbacks...)
	return out
}

type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	DevMode       bool

	// Server-wide secret mixed into the scrypt KDF for the encrypted mnemonic
	// blob, so a database dump alone cannot be brute-forced offline against a
	// weak PIN. Required in production.
	KeystorePepper string

	// RPC endpoints, one per supported chain. Mainnet needs these set; dev-mode
	// falls back to well-known public RPCs so the smoke tests pass without an
	// account at Infura/Alchemy.
	RPCs map[string]RPC

	// Third-party API keys for history / NFT / swap / bridge endpoints.
	EtherscanKey    string
	BscscanKey      string
	PolygonscanKey  string
	ArbiscanKey     string
	BasescanKey     string
	AlchemyNFTKey   string
	MoralisKey      string
	OneInchKey      string
	LifiKey         string
	// CoinGeckoKey is optional; CoinGecko works unauthenticated with rate limits.
	CoinGeckoKey string

	// WalletConnect v2 project id — used by the Flutter DApp browser to
	// authenticate against the relay. The server does not itself connect to
	// WC; it holds the project id for the client to read.
	WalletConnectProjectID string

	// PIN rate limit: lock the wallet for LockMinutes after MaxPINAttempts
	// consecutive failures. Defaults: 5 attempts, 15 min.
	MaxPINAttempts int
	LockMinutes    int

	// High-value threshold in USD above which a transaction requires a second
	// 2FA (asset service's /account/verify-sensitive). <=0 disables.
	HighValueUSDThreshold float64

	AssetURL   string
	AccountURL string
}

func LoadConfig() Config {
	c := Config{
		Port:          common.Env("PORT", "8097"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		DevMode:       common.Env("DEV_MODE", "") == "true",

		KeystorePepper: common.Env("WEB3_KEYSTORE_PEPPER", ""),

		EtherscanKey:   common.Env("ETHERSCAN_API_KEY", ""),
		BscscanKey:     common.Env("BSCSCAN_API_KEY", ""),
		PolygonscanKey: common.Env("POLYGONSCAN_API_KEY", ""),
		ArbiscanKey:    common.Env("ARBISCAN_API_KEY", ""),
		BasescanKey:    common.Env("BASESCAN_API_KEY", ""),
		AlchemyNFTKey:  common.Env("ALCHEMY_NFT_KEY", ""),
		MoralisKey:     common.Env("MORALIS_API_KEY", ""),
		OneInchKey:     common.Env("ONEINCH_API_KEY", ""),
		LifiKey:        common.Env("LIFI_API_KEY", ""),
		CoinGeckoKey:   common.Env("COINGECKO_API_KEY", ""),

		WalletConnectProjectID: common.Env("WALLETCONNECT_PROJECT_ID", ""),

		MaxPINAttempts:        common.EnvInt("WEB3_MAX_PIN_ATTEMPTS", 5),
		LockMinutes:           common.EnvInt("WEB3_LOCK_MINUTES", 15),
		HighValueUSDThreshold: envFloat("WEB3_HIGH_VALUE_USD", 10000),

		AssetURL:   common.Env("ASSET_INTERNAL_URL", "http://localhost:8082"),
		AccountURL: common.Env("ACCOUNT_INTERNAL_URL", "http://localhost:8081"),
	}
	c.RPCs = map[string]RPC{
		ChainETH:      loadRPC("ETH_RPC_URL", "https://cloudflare-eth.com"),
		ChainBSC:      loadRPC("BSC_RPC_URL", "https://bsc-dataseed.binance.org"),
		ChainPolygon:  loadRPC("POLYGON_RPC_URL", "https://polygon-rpc.com"),
		ChainArbitrum: loadRPC("ARBITRUM_RPC_URL", "https://arb1.arbitrum.io/rpc"),
		ChainBase:     loadRPC("BASE_RPC_URL", "https://mainnet.base.org"),
		ChainSolana:   loadRPC("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"),
		ChainTRON:     loadRPC("TRON_RPC_URL", "https://api.trongrid.io"),
		ChainBTC:      loadRPC("BTC_RPC_URL", "https://blockstream.info/api"),
	}
	return c
}

func loadRPC(primaryKey, def string) RPC {
	p := common.Env(primaryKey, def)
	fb := common.Env(primaryKey+"_FALLBACKS", "")
	var list []string
	if fb != "" {
		for _, s := range strings.Split(fb, ",") {
			if s = strings.TrimSpace(s); s != "" {
				list = append(list, s)
			}
		}
	}
	return RPC{Primary: p, Fallbacks: list}
}

func envFloat(key string, def float64) float64 {
	s := common.Env(key, "")
	if s == "" {
		return def
	}
	var f float64
	_, err := fmtSscan(s, &f)
	if err != nil {
		return def
	}
	return f
}
