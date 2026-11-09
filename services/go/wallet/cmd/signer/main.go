// Command signer 是热钱包签名进程，独立容器。它持有热钱包/充值地址的唯一种子
// （keystore：scrypt + AES-GCM），经带鉴权的 HTTP 签名。钱包进程只见公钥和签名。
// 换机器时把 WALLET_SIGNER_URL 指过来即可。日志不打印密钥。
//
//	signer init            创建 keystore（拒绝覆盖）
//	signer xpub            把账户公钥打成环境变量行
//	signer sol-addresses   把 N 个 SOL 充值地址写到文件
//	signer serve           提供 /v1/pubkey 和 /v1/sign
//
// 环境变量：
//
//	WALLET_KEYSTORE                  keystore 路径（默认 /data/hot-keystore.json）
//	WALLET_KEYSTORE_PASSPHRASE_FILE  口令文件（优先）
//	WALLET_KEYSTORE_PASSPHRASE       口令（回退）
//	WALLET_SIGNER_TOKEN              钱包服务出示的 bearer（>= 24 字符）
//	WALLET_SIGNER_ADDR               监听地址（默认 :8095）
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	_ "github.com/linbit/platform/services/go/common" // log redaction
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
