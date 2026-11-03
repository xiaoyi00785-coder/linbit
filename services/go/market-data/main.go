// Command market-data is the Linbit market-data service. It consumes the
// matching engine's trade and depth events over a replaceable inbound-messaging
// interface (locally: an HTTP ingest endpoint fed by the engine's HttpSink),
// aggregates K-lines (multiple intervals), order-book depth, 24h tickers and
// recent trades, serves them over REST (consistent with docs/api/openapi.yaml)
// and pushes them over WebSocket (docs/api/websocket.md). K-lines are persisted
// to PostgreSQL (goose migrations) so the service recovers its state on restart.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/linbit/platform/services/go/common"
)

func main() {
	cfg := LoadConfig()
	common.RequireProduction("market-data", nil, "DATABASE_URL", "INTERNAL_TOKEN", "REDIS_URL")

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)

	if err := waitForDB(db); err != nil {
		log.Fatalf("db not reachable: %v", err)
	}
	if err := Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if client := common.MaybeRedis(cfg.RedisURL); client != nil {
		defer client.Close()
	}

	store := NewStore(db)

	ctx := context.Background()
	symbols := resolveSymbols(ctx, cfg, store)
	log.Printf("market-data tracking symbols: %v", symbols)

	agg := NewAggregator(store, symbols)
	// Load per-symbol precision from the shared `symbols` table (same source the
	// order service uses) so REST/WS depth and ticker strings are padded to each
	// pair's price/quantity rules.
	if infos, err := store.Symbols(ctx); err != nil {
		log.Printf("load symbol precision: %v", err) // non-fatal; falls back to raw strings
	} else {
		agg.SetPrecisions(infos)
	}
	if err := agg.Recover(ctx); err != nil {
		log.Printf("recover klines: %v", err) // non-fatal; start empty
	}
	hub := NewHub(agg)
	agg.SetBroadcaster(hub)
	agg.StartPersister(ctx)

	// Best-effort depth recovery: pull a full book snapshot from the engine so
	// depth is correct immediately after a restart (live deltas follow).
	bootstrapDepth(cfg, symbols, agg)

	ingest := NewHTTPEventSource(agg, cfg.InternalToken)
	srv := NewServer(cfg, store, agg, hub, ingest)

	addr := ":" + cfg.Port
	log.Printf("market-data service listening on %s (engine=%s)", addr, cfg.EngineURL)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

// resolveSymbols picks the tracked symbols: explicit config wins, then the
// shared symbols table, then a sane default.
func resolveSymbols(ctx context.Context, cfg Config, store *Store) []string {
	if len(cfg.Symbols) > 0 {
		return cfg.Symbols
	}
	if infos, err := store.Symbols(ctx); err == nil && len(infos) > 0 {
		out := make([]string, 0, len(infos))
		for _, si := range infos {
			out = append(out, si.Symbol)
		}
		return out
	}
	return []string{"BTCUSDT", "ETHUSDT"}
}

// bootstrapDepth fetches a full order-book snapshot per symbol from the engine
// and seeds the aggregator. Failures are logged and ignored (the book then
// rebuilds from live deltas).
func bootstrapDepth(cfg Config, symbols []string, agg *Aggregator) {
	client := &http.Client{Timeout: 3 * time.Second}
	for _, sym := range symbols {
		req, err := http.NewRequest(http.MethodGet, cfg.EngineURL+"/engine/depth?symbol="+sym, nil)
		if err != nil {
			continue
		}
		if cfg.InternalToken != "" {
			req.Header.Set("X-Internal-Token", cfg.InternalToken)
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("bootstrap depth %s: %v", sym, err)
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var snap struct {
				Symbol   string      `json:"symbol"`
				Sequence uint64      `json:"sequence"`
				Bids     []wireLevel `json:"bids"`
				Asks     []wireLevel `json:"asks"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
				return
			}
			agg.OnDepth(DepthEvent{
				Symbol:   sym,
				Sequence: snap.Sequence,
				Snapshot: true,
				Bids:     toLevels(snap.Bids),
				Asks:     toLevels(snap.Asks),
			})
		}()
	}
}

func waitForDB(db *sql.DB) error {
	var err error
	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return err
}
