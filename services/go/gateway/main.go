// Command gateway is the Linbit API gateway: JWT authentication plus reverse
// proxying to the account and asset services.
package main

import (
	"log"
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

func main() {
	cfg := LoadConfig()

	if client := common.MaybeRedis(cfg.RedisURL); client != nil {
		defer client.Close()
	}

	gw, err := NewGateway(cfg)
	if err != nil {
		log.Fatalf("build gateway: %v", err)
	}
	addr := ":" + cfg.Port
	log.Printf("gateway listening on %s (account=%s asset=%s)", addr, cfg.AccountURL, cfg.AssetURL)
	if err := http.ListenAndServe(addr, gw.Handler()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
