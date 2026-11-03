// Command signer is the Linbit hot-wallet signer. It holds the only copy of the
// hot/deposit key seed in an encrypted keystore (scrypt + AES-GCM) and signs
// over an authenticated HTTP API; the wallet service only ever sees public
// keys and signatures. It runs as its own container today and can be moved to
// a dedicated host later by pointing WALLET_SIGNER_URL at it.
//
//	signer init            create the keystore (refuses to overwrite)
//	signer xpub            print the public account keys as env lines
//	signer sol-addresses   write N public SOL deposit addresses to a file
//	signer serve           serve /v1/pubkey and /v1/sign
//
// Key material is never printed. Configuration (env):
//
//	WALLET_KEYSTORE                  keystore path (default /data/hot-keystore.json)
//	WALLET_KEYSTORE_PASSPHRASE_FILE  file holding the passphrase (preferred)
//	WALLET_KEYSTORE_PASSPHRASE       passphrase (fallback)
//	WALLET_SIGNER_TOKEN              bearer token the wallet service presents (>= 24 chars)
//	WALLET_SIGNER_ADDR               listen address (default :8095)
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/wallet/chainkit"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func passphrase() ([]byte, error) {
	if f := os.Getenv("WALLET_KEYSTORE_PASSPHRASE_FILE"); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		return bytes.TrimRight(b, "\r\n"), nil
	}
	if p := os.Getenv("WALLET_KEYSTORE_PASSPHRASE"); p != "" {
		return []byte(p), nil
	}
	return nil, errors.New("set WALLET_KEYSTORE_PASSPHRASE_FILE or WALLET_KEYSTORE_PASSPHRASE")
}

func open() *chainkit.Keystore {
	pass, err := passphrase()
	if err != nil {
		log.Fatal(err)
	}
	ks, err := chainkit.OpenKeystore(env("WALLET_KEYSTORE", "/data/hot-keystore.json"), pass)
	if err != nil {
		log.Fatalf("open keystore: %v", err)
	}
	return ks
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: signer init|xpub|sol-addresses|serve")
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "init":
		fs := flag.NewFlagSet("init", flag.ExitOnError)
		mainnet := fs.Bool("mainnet", false, "derive mainnet paths/versions (default testnet)")
		fs.Parse(args)
		pass, err := passphrase()
		if err != nil {
			log.Fatal(err)
		}
		path := env("WALLET_KEYSTORE", "/data/hot-keystore.json")
		if _, err := chainkit.CreateKeystore(path, pass, !*mainnet); err != nil {
			log.Fatalf("init: %v", err)
		}
		log.Printf("keystore created at %s (testnet=%v). Back up the file and passphrase separately.", path, !*mainnet)
	case "xpub":
		ks := open()
		for _, c := range []chainkit.Chain{chainkit.ChainEVM, chainkit.ChainTron, chainkit.ChainBTC} {
			x, err := ks.AccountXpub(c)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("WALLET_XPUB_%s=%s\n", strings.ToUpper(string(c)), x)
		}
	case "sol-addresses":
		fs := flag.NewFlagSet("sol-addresses", flag.ExitOnError)
		n := fs.Int("n", 10000, "number of deposit addresses")
		out := fs.String("o", "sol-deposit-addresses.txt", "output file")
		fs.Parse(args)
		dep, hot, err := open().SolAddresses(*n)
		if err != nil {
			log.Fatal(err)
		}
		body := "# Linbit SOL deposit addresses (public). Index = line number - 1.\n" + strings.Join(dep, "\n") + "\n"
		if err := os.WriteFile(*out, []byte(body), 0o644); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %d SOL deposit addresses to %s; hot address %s", len(dep), *out, hot)
	case "serve":
		token := os.Getenv("WALLET_SIGNER_TOKEN")
		if len(token) < 24 {
			log.Fatal("WALLET_SIGNER_TOKEN must be set (>= 24 random characters)")
		}
		ks := open()
		h := chainkit.SignerHandler(chainkit.NewLocalHotSigner(ks), token)
		for _, c := range []chainkit.Chain{chainkit.ChainEVM, chainkit.ChainTron, chainkit.ChainBTC, chainkit.ChainSol} {
			pk, err := chainkit.NewLocalHotSigner(ks).PublicKey(context.Background(), c, chainkit.BranchHot, 0)
			if err == nil {
				hrp := "tb"
				if !ks.Testnet() {
					hrp = "bc"
				}
				a, _ := chainkit.AddressFromPub(c, pk, hrp)
				log.Printf("hot %s address %s", c, a)
			}
		}
		addr := env("WALLET_SIGNER_ADDR", ":8095")
		srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second}
		log.Printf("signer listening on %s (testnet=%v)", addr, ks.Testnet())
		log.Fatal(srv.ListenAndServe())
	default:
		log.Fatalf("unknown command %q", cmd)
	}
}
