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
	pgPort      = envInt("DEVSTACK_PG_PORT", 5433)
	gatewayPort = envOr("DEVSTACK_GATEWAY_PORT", "8080")
	accountPort = envOr("DEVSTACK_ACCOUNT_PORT", "8081")
	assetPort   = envOr("DEVSTACK_ASSET_PORT", "8082")
	enginePort  = envOr("DEVSTACK_ENGINE_PORT", "8083")
	orderPort   = envOr("DEVSTACK_ORDER_PORT", "8084")
	marketPort  = envOr("DEVSTACK_MARKET_PORT", "8085")
	walletPort  = envOr("DEVSTACK_WALLET_PORT", "8086")
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

	services := []string{"account", "asset", "gateway", "order", "market-data", "wallet"}
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
			// Dev mode exposes the mock-chain injection/advance endpoints so smoke
			// scripts can simulate deposits and block confirmations.
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
	}

	if execCmd == "" {
		// Standalone mode: block until interrupted.
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		fmt.Println("devstack running; press Ctrl-C to stop")
		for _, e := range stackEnv {
			fmt.Println("  " + e)
		}
		<-sig
		return 0
	}

	// Exec mode: run the command with the stack env, then tear down.
	log.Printf("devstack: exec %q", execCmd)
	c := exec.Command("sh", "-c", execCmd)
	c.Dir = absRoot // run the command from the repository root
	c.Env = append(os.Environ(), stackEnv...)
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
