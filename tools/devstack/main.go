// Command devstack boots a local Linbit backend for development and testing
// without Docker: a real embedded PostgreSQL (github.com/fergusstrange/embedded-postgres),
// an in-memory Redis (miniredis), and the gateway/account/asset services built
// and run as child processes.
//
// Usage:
//
//	go run ./tools/devstack                 # boot and block until Ctrl-C
//	go run ./tools/devstack -exec "<cmd>"   # boot, run <cmd>, tear down, exit with its code
//
// When -exec is used the following environment variables are exported to the
// command: GATEWAY_URL, ACCOUNT_URL, ASSET_URL, DATABASE_URL, REDIS_URL.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"

	"github.com/alicebob/miniredis/v2"
)

const (
	jwtSecret   = "devstack-insecure-jwt-secret"
	internalTok = "devstack-internal-token"
)

// Ports are overridable via environment so two devstacks (e.g. two parallel
// smoke runs) can coexist without colliding. Set DEVSTACK_*_PORT to change them.
var (
	pgPort       = envInt("DEVSTACK_PG_PORT", 5433)
	gatewayPort  = envOr("DEVSTACK_GATEWAY_PORT", "8080")
	accountPort  = envOr("DEVSTACK_ACCOUNT_PORT", "8081")
	assetPort    = envOr("DEVSTACK_ASSET_PORT", "8082")
	enginePort   = envOr("DEVSTACK_ENGINE_PORT", "8083")
	orderPort    = envOr("DEVSTACK_ORDER_PORT", "8084")
	marketPort   = envOr("DEVSTACK_MARKET_PORT", "8085")
	walletPort   = envOr("DEVSTACK_WALLET_PORT", "8086")
	kycPort      = envOr("DEVSTACK_KYC_PORT", "8087")
	strategyPort = envOr("DEVSTACK_STRATEGY_PORT", "8090")
	growthPort   = envOr("DEVSTACK_GROWTH_PORT", "8091")
	earnPort     = envOr("DEVSTACK_EARN_PORT", "8092")
	adminPort    = envOr("DEVSTACK_ADMIN_PORT", "8088")
	futuresPort  = envOr("DEVSTACK_FUTURES_PORT", "8089")
	mmPort       = envOr("DEVSTACK_MM_PORT", "8093")
	// Fake Telegram API for alert smokes (smoke-mm listens here); real tokens are never used.
	tgAPI = envOr("DEVSTACK_TELEGRAM_API_URL", "http://127.0.0.1:18099")
)

// envOr returns the env var value or def when unset/empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt returns the integer env var or def when unset/invalid.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func main() {
	root := flag.String("root", ".", "repository root (where services/go lives)")
	execCmd := flag.String("exec", "", "command to run once the stack is ready; stack is torn down afterwards")
	flag.Parse()

	os.Exit(run(*root, *execCmd))
}

func run(root, execCmd string) int {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		log.Printf("resolve root: %v", err)
		return 1
	}

	// --- PostgreSQL ---------------------------------------------------------
	runtimePath, err := os.MkdirTemp("", "linbit-pg-*")
	if err != nil {
		log.Printf("temp dir: %v", err)
		return 1
	}
	defer os.RemoveAll(runtimePath)

	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Username("linbit").Password("linbit").Database("linbit").
		Port(uint32(pgPort)).
		RuntimePath(runtimePath).
		StartTimeout(90 * time.Second))

	log.Printf("devstack: starting embedded postgres on :%d ...", pgPort)
	if err := pg.Start(); err != nil {
		log.Printf("start postgres: %v", err)
		return 1
	}
	defer func() {
		log.Printf("devstack: stopping postgres")
		_ = pg.Stop()
	}()
	databaseURL := fmt.Sprintf("postgres://linbit:linbit@localhost:%d/linbit?sslmode=disable", pgPort)

	// --- Redis (optional, in-memory) ---------------------------------------
	mr, err := miniredis.Run()
	if err != nil {
		log.Printf("start miniredis: %v", err)
		return 1
	}
	defer mr.Close()
	redisURL := "redis://" + mr.Addr()
	log.Printf("devstack: miniredis on %s", mr.Addr())

	// --- Build services -----------------------------------------------------
	binDir, err := os.MkdirTemp("", "linbit-bin-*")
	if err != nil {
		log.Printf("temp bin dir: %v", err)
		return 1
	}
	defer os.RemoveAll(binDir)

	services := []string{"account", "asset", "gateway", "order", "market-data", "wallet", "kyc", "admin", "futures", "strategy", "growth", "earn", "marketmaker"}
	bins := map[string]string{}
	for _, name := range services {
		bin := filepath.Join(binDir, name)
		log.Printf("devstack: building %s ...", name)
		build := exec.Command("go", "build", "-o", bin, ".")
		build.Dir = filepath.Join(absRoot, "services", "go", name)
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			log.Printf("build %s: %v", name, err)
			return 1
		}
		bins[name] = bin
	}

	// --- Build the Rust matching engine (cargo, debug build) ----------------
	engineDir := filepath.Join(absRoot, "services", "rust", "matching-engine")
	log.Printf("devstack: building matching-engine (cargo) ...")
	cargoBuild := exec.Command("cargo", "build", "--bin", "matching-engine")
	cargoBuild.Dir = engineDir
	cargoBuild.Stdout, cargoBuild.Stderr = os.Stdout, os.Stderr
	if err := cargoBuild.Run(); err != nil {
		log.Printf("build matching-engine: %v", err)
		return 1
	}
	bins["engine"] = filepath.Join(engineDir, "target", "debug", "matching-engine")

	// Per-run data dir for the engine's WAL + snapshots (thrown away on exit).
	engineData, err := os.MkdirTemp("", "linbit-engine-*")
	if err != nil {
		log.Printf("engine data dir: %v", err)
		return 1
	}
	defer os.RemoveAll(engineData)

	// Per-run data dir for account avatar uploads (thrown away on exit).
	avatarData, err := os.MkdirTemp("", "linbit-avatars-*")
	if err != nil {
		log.Printf("avatar data dir: %v", err)
		return 1
	}
	defer os.RemoveAll(avatarData)

	// Per-run data dir for KYC document uploads (thrown away on exit).
	kycData, err := os.MkdirTemp("", "linbit-kyc-*")
	if err != nil {
		log.Printf("kyc data dir: %v", err)
		return 1
	}
	defer os.RemoveAll(kycData)

	// Per-run bootstrap super admin for the admin service. Random unless set in
	// the environment; handed to -exec commands, never printed.
	adminUser := envOr("ADMIN_BOOTSTRAP_USERNAME", "devadmin")
	adminPass := envOr("ADMIN_BOOTSTRAP_PASSWORD", randToken(18))
	adminTOTP := envOr("ADMIN_BOOTSTRAP_TOTP_SECRET", randBase32(20))

	// --- Run services -------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	envs := map[string][]string{
		"asset": {
			"PORT=" + assetPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"REDIS_URL=" + redisURL,
		},
		"engine": {
			"BIND=127.0.0.1:" + enginePort,
			"INTERNAL_TOKEN=" + internalTok,
			"DATA_DIR=" + engineData,
			"SNAPSHOT_EVERY=1000",
			"SYMBOLS=BTCUSDT:6,ETHUSDT:5,SOLUSDT:3,BNBUSDT:3,TRXUSDT:1,AAPLUSDT:4,TSLAUSDT:4,NVDAUSDT:4,MSFTUSDT:4,GOOGLUSDT:4,AMZNUSDT:4,METAUSDT:4,COINUSDT:4,SPYUSDT:4,PAXGUSDT:5,XAUTUSDT:5",
			// Stream trade/depth events to market-data (local pub/sub stand-in).
			"EVENT_SINK_URL=http://localhost:" + marketPort + "/internal/events",
		},
		"order": {
			"PORT=" + orderPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"ASSET_INTERNAL_URL=http://localhost:" + assetPort,
			"ENGINE_URL=http://localhost:" + enginePort,
			"REDIS_URL=" + redisURL,
		},
		"market-data": {
			"PORT=" + marketPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"ENGINE_URL=http://localhost:" + enginePort,
			"REDIS_URL=" + redisURL,
			// Deterministic smokes: index = local last price unless pinned via
			// /internal/dev/index (smoke-mm). Override with DEVSTACK_INDEX_FEED.
			"INDEX_FEED=" + envOr("DEVSTACK_INDEX_FEED", "off"),
			"DEV_MODE=true",
			// TradFi deviation alerts.
			"TELEGRAM_API_URL=" + tgAPI,
			"TELEGRAM_BOT_TOKEN=devstack-telegram-token",
			"TELEGRAM_CHAT_ID=devstack-chat",
			// Live APR for earn-linked home placements.
			"EARN_URL=http://localhost:" + earnPort,
			// Chat: KYC level for posting, futures contracts for rooms.
			"ACCOUNT_URL=http://localhost:" + accountPort,
			"FUTURES_URL=http://localhost:" + futuresPort,
		},
		"account": {
			"PORT=" + accountPort,
			"DATABASE_URL=" + databaseURL,
			"JWT_SECRET=" + jwtSecret,
			"ASSET_INTERNAL_URL=http://localhost:" + assetPort,
			"INTERNAL_TOKEN=" + internalTok,
			"REDIS_URL=" + redisURL,
			// Dev mode exposes the internal "last verification code" endpoint so
			// smoke scripts can complete code-gated flows without a real provider.
			"DEV_MODE=true",
			// Avatar uploads land in a per-run temp dir (local ObjectStorage).
			"AVATAR_STORAGE_DIR=" + avatarData,
		},
		"wallet": {
			"PORT=" + walletPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"REDIS_URL=" + redisURL,
			"ACCOUNT_INTERNAL_URL=http://localhost:" + accountPort,
			"ASSET_INTERNAL_URL=http://localhost:" + assetPort,
			"MARKET_URL=http://localhost:" + marketPort,
			// Dev mode exposes the mock-chain injection/advance endpoints so smoke
			// scripts can simulate deposits and block confirmations.
			"DEV_MODE=true",
		},
		"kyc": {
			"PORT=" + kycPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"REDIS_URL=" + redisURL,
			"ACCOUNT_INTERNAL_URL=http://localhost:" + accountPort,
			// KYC document uploads land in a per-run temp dir (local ObjectStorage).
			"KYC_DOC_STORAGE_DIR=" + kycData,
		},
		"admin": {
			"PORT=" + adminPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"ACCOUNT_INTERNAL_URL=http://localhost:" + accountPort,
			"ASSET_INTERNAL_URL=http://localhost:" + assetPort,
			"WALLET_INTERNAL_URL=http://localhost:" + walletPort,
			"KYC_INTERNAL_URL=http://localhost:" + kycPort,
			"ORDER_INTERNAL_URL=http://localhost:" + orderPort,
			"MARKET_URL=http://localhost:" + marketPort,
			"FUTURES_INTERNAL_URL=http://localhost:" + futuresPort,
			"GROWTH_INTERNAL_URL=http://localhost:" + growthPort,
			"EARN_INTERNAL_URL=http://localhost:" + earnPort,
			"ADMIN_BOOTSTRAP_USERNAME=" + adminUser,
			"ADMIN_BOOTSTRAP_PASSWORD=" + adminPass,
			"ADMIN_BOOTSTRAP_TOTP_SECRET=" + adminTOTP,
			"MM_INTERNAL_URL=http://localhost:" + mmPort,
			"SOLVENCY_HOUR_UTC=-1",
			"TELEGRAM_API_URL=" + tgAPI,
			"TELEGRAM_BOT_TOKEN=devstack-telegram-token",
			"TELEGRAM_CHAT_ID=devstack-chat",
			// Dev mode exposes the scheduler "run at now=X" endpoint so the smoke
			// can exercise the 24h fee-change notice without waiting.
			"DEV_MODE=true",
		},
		"marketmaker": {
			"PORT=" + mmPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"ORDER_URL=http://localhost:" + orderPort,
			"FUTURES_URL=http://localhost:" + futuresPort,
			"MARKET_URL=http://localhost:" + marketPort,
			"ASSET_URL=http://localhost:" + assetPort,
			"WALLET_CHAIN=mock",
			"TELEGRAM_API_URL=" + tgAPI,
			"TELEGRAM_BOT_TOKEN=devstack-telegram-token",
			"TELEGRAM_CHAT_ID=devstack-chat",
		},
		"futures": {
			"PORT=" + futuresPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"ASSET_INTERNAL_URL=http://localhost:" + assetPort,
			"MARKET_URL=http://localhost:" + marketPort,
			"KYC_URL=http://localhost:" + kycPort,
			// Dev mode exposes index override / forced tick / funding endpoints.
			"DEV_MODE=true",
		},
		"strategy": {
			"PORT=" + strategyPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"ORDER_URL=http://localhost:" + orderPort,
			"MARKET_URL=http://localhost:" + marketPort,
			// Fast polling / sampling and an open strategy square for the smoke.
			"GRID_POLL_MS=300",
			"GRID_SNAPSHOT_SECONDS=1",
			"GRID_SQUARE_MIN_RUN_SECONDS=0",
			"DEV_MODE=true",
		},
		"growth": {
			"PORT=" + growthPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"ACCOUNT_INTERNAL_URL=http://localhost:" + accountPort,
			"ASSET_INTERNAL_URL=http://localhost:" + assetPort,
			"ORDER_INTERNAL_URL=http://localhost:" + orderPort,
			"FUTURES_INTERNAL_URL=http://localhost:" + futuresPort,
			"GROWTH_SYNC_MS=500",
			// Dev mode exposes the manual sync / settle endpoints.
			"DEV_MODE=true",
		},
		"earn": {
			"PORT=" + earnPort,
			"DATABASE_URL=" + databaseURL,
			"INTERNAL_TOKEN=" + internalTok,
			"ASSET_INTERNAL_URL=http://localhost:" + assetPort,
			"FUTURES_INTERNAL_URL=http://localhost:" + futuresPort,
			"EARN_TICK_MS=1000",
			// Dev mode exposes the clock-advance endpoint.
			"DEV_MODE=true",
		},
		"gateway": {
			"PORT=" + gatewayPort,
			"JWT_SECRET=" + jwtSecret,
			"ACCOUNT_URL=http://localhost:" + accountPort,
			"ASSET_URL=http://localhost:" + assetPort,
			"WALLET_URL=http://localhost:" + walletPort,
			"ORDER_URL=http://localhost:" + orderPort,
			"MARKET_URL=http://localhost:" + marketPort,
			"KYC_URL=http://localhost:" + kycPort,
			"INTERNAL_TOKEN=" + internalTok,
			"ADMIN_URL=http://localhost:" + adminPort,
			"FUTURES_URL=http://localhost:" + futuresPort,
			"STRATEGY_URL=http://localhost:" + strategyPort,
			"GROWTH_URL=http://localhost:" + growthPort,
			"EARN_URL=http://localhost:" + earnPort,
			"REDIS_URL=" + redisURL,
		},
	}

	// Start order matters: asset first (account/order call it), then the engine,
	// then the order service (migrates the spot schema, needs asset+engine), then
	// market-data (reads the symbols table order seeded), then account, wallet
	// (calls account+asset), then gateway. Each is started and awaited healthy
	// before the next so their startup migrations run sequentially.
	startOrder := []struct {
		name   string
		health string
	}{
		{"asset", "http://localhost:" + assetPort + "/healthz"},
		{"engine", "http://localhost:" + enginePort + "/healthz"},
		{"order", "http://localhost:" + orderPort + "/healthz"},
		{"market-data", "http://localhost:" + marketPort + "/healthz"},
		{"account", "http://localhost:" + accountPort + "/healthz"},
		{"wallet", "http://localhost:" + walletPort + "/healthz"},
		{"kyc", "http://localhost:" + kycPort + "/healthz"},
		{"admin", "http://localhost:" + adminPort + "/healthz"},
		{"futures", "http://localhost:" + futuresPort + "/healthz"},
		{"strategy", "http://localhost:" + strategyPort + "/healthz"},
		{"growth", "http://localhost:" + growthPort + "/healthz"},
		{"earn", "http://localhost:" + earnPort + "/healthz"},
		{"marketmaker", "http://localhost:" + mmPort + "/healthz"},
		{"gateway", "http://localhost:" + gatewayPort + "/healthz"},
	}

	var procs []*exec.Cmd
	defer func() {
		cancel()
		for _, p := range procs {
			if p.Process != nil {
				_ = p.Process.Signal(syscall.SIGTERM)
			}
		}
		for _, p := range procs {
			_ = p.Wait()
		}
	}()

	for _, svc := range startOrder {
		cmd := exec.CommandContext(ctx, bins[svc.name])
		cmd.Env = append(os.Environ(), envs[svc.name]...)
		cmd.Stdout = prefixed(svc.name, os.Stdout)
		cmd.Stderr = prefixed(svc.name, os.Stderr)
		if err := cmd.Start(); err != nil {
			log.Printf("start %s: %v", svc.name, err)
			return 1
		}
		procs = append(procs, cmd)
		if err := waitHealthy(svc.health, 60*time.Second); err != nil {
			log.Printf("%s not healthy: %v", svc.name, err)
			return 1
		}
		log.Printf("devstack: %s healthy", svc.name)
	}

	gatewayURL := "http://localhost:" + gatewayPort
	log.Printf("devstack: READY  gateway=%s", gatewayURL)

	stackEnv := []string{
		"GATEWAY_URL=" + gatewayURL,
		"ACCOUNT_URL=http://localhost:" + accountPort,
		"ASSET_URL=http://localhost:" + assetPort,
		"WALLET_URL=http://localhost:" + walletPort,
		"ORDER_URL=http://localhost:" + orderPort,
		"ENGINE_URL=http://localhost:" + enginePort,
		"MARKET_URL=http://localhost:" + marketPort,
		"DATABASE_URL=" + databaseURL,
		"REDIS_URL=" + redisURL,
		// Dev-only shared secret so smoke scripts can reach asset's /internal/*
		// (e.g. seed balances). Not a real credential; never used in production.
		"INTERNAL_TOKEN=" + internalTok,
		"ADMIN_URL=http://localhost:" + adminPort,
		"FUTURES_INTERNAL_URL=http://localhost:" + futuresPort,
		// Smokes read FUTURES_URL (default :8089); export it so shifted
		// DEVSTACK_FUTURES_PORT never hits another stack on the default port.
		"FUTURES_URL=http://localhost:" + futuresPort,
		"MM_URL=http://localhost:" + mmPort,
		"GROWTH_URL=http://localhost:" + growthPort,
		"EARN_URL=http://localhost:" + earnPort,
		"DEVSTACK_TELEGRAM_API_URL=" + tgAPI,
	}
	// Bootstrap super-admin credentials go only to exec'd commands (smokes),
	// never to stdout.
	adminEnv := []string{
		"ADMIN_BOOTSTRAP_USERNAME=" + adminUser,
		"ADMIN_BOOTSTRAP_PASSWORD=" + adminPass,
		"ADMIN_BOOTSTRAP_TOTP_SECRET=" + adminTOTP,
	}

	if execCmd == "" {
		// Standalone mode (manual / frontend testing): INDEX_FEED=off means
		// index = local last trade, which is empty on a fresh stack, so loan
		// quotes and futures fail with PRICE_UNAVAILABLE. Pin dev index prices
		// for the default collateral/futures pairs. DEVSTACK_INDEX_SEED="none"
		// disables; format "BTCUSDT=50000,ETHUSDT=2500". Smokes (-exec) pin
		// their own prices and are not affected.
		seedDevIndex("http://localhost:"+marketPort, envOr("DEVSTACK_INDEX_SEED", "BTCUSDT=50000,ETHUSDT=2500,SOLUSDT=150,BNBUSDT=600,TRXUSDT=0.12"))
		// Standalone mode: block until interrupted.
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		fmt.Println("devstack running; press Ctrl-C to stop")
		for _, e := range stackEnv {
			fmt.Println("  " + e)
		}
		fmt.Println("  ADMIN_BOOTSTRAP_*: random per run unless set in the environment")
		<-sig
		return 0
	}

	// Exec mode: run the command with the stack env, then tear down.
	log.Printf("devstack: exec %q", execCmd)
	c := exec.Command("sh", "-c", execCmd)
	c.Dir = absRoot // run the command from the repository root
	c.Env = append(append(os.Environ(), stackEnv...), adminEnv...)
	c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		log.Printf("exec: %v", err)
		return 1
	}
	return 0
}

func waitHealthy(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(300 * time.Millisecond)
	}
	return lastErr
}

// prefixed returns a writer that tags each line with the service name.
func prefixed(name string, w io.Writer) io.Writer {
	pr, pw := io.Pipe()
	go func() {
		buf := make([]byte, 4096)
		prefix := []byte("[" + name + "] ")
		atLineStart := true
		for {
			n, err := pr.Read(buf)
			for i := 0; i < n; i++ {
				if atLineStart {
					w.Write(prefix)
					atLineStart = false
				}
				w.Write(buf[i : i+1])
				if buf[i] == '\n' {
					atLineStart = true
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return pw
}

// randToken returns n random bytes, hex encoded.
func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// randBase32 returns n random bytes as unpadded base32 (a TOTP secret).
func randBase32(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// seedDevIndex pins dev index prices on market-data (DEV_MODE only).
func seedDevIndex(marketURL, spec string) {
	if spec == "" || spec == "none" {
		return
	}
	for _, kv := range strings.Split(spec, ",") {
		sym, px, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			continue
		}
		body := fmt.Sprintf(`{"symbol":%q,"price":%q}`, strings.ToUpper(sym), px)
		req, _ := http.NewRequest(http.MethodPost, marketURL+"/internal/dev/index", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", internalTok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("devstack: index seed %s: %v", sym, err)
			continue
		}
		resp.Body.Close()
		log.Printf("devstack: dev index %s=%s (%d)", sym, px, resp.StatusCode)
	}
}
