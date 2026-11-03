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
	"syscall"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"

	"github.com/alicebob/miniredis/v2"
)

const (
	pgPort      = 5433
	gatewayPort = "8080"
	accountPort = "8081"
	assetPort   = "8082"
	jwtSecret   = "devstack-insecure-jwt-secret"
	internalTok = "devstack-internal-token"
)

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
		Port(pgPort).
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

	services := []string{"account", "asset", "gateway"}
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
		"account": {
			"PORT=" + accountPort,
			"DATABASE_URL=" + databaseURL,
			"JWT_SECRET=" + jwtSecret,
			"ASSET_INTERNAL_URL=http://localhost:" + assetPort,
			"INTERNAL_TOKEN=" + internalTok,
			"REDIS_URL=" + redisURL,
		},
		"gateway": {
			"PORT=" + gatewayPort,
			"JWT_SECRET=" + jwtSecret,
			"ACCOUNT_URL=http://localhost:" + accountPort,
			"ASSET_URL=http://localhost:" + assetPort,
			"REDIS_URL=" + redisURL,
		},
	}

	// Start order matters: asset first (account calls it on register), then
	// account, then gateway. Each is started and awaited healthy before the
	// next so their startup migrations run sequentially, not concurrently.
	startOrder := []struct {
		name   string
		health string
	}{
		{"asset", "http://localhost:" + assetPort + "/healthz"},
		{"account", "http://localhost:" + accountPort + "/healthz"},
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
		"DATABASE_URL=" + databaseURL,
		"REDIS_URL=" + redisURL,
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
