// Command reconcile is a read-only report of spot orders the engine and the
// database disagree about, plus unresolved settlement dead letters.
//
// It never freezes, unfreezes, settles, or updates a row. An operator uses the
// list to decide what to repair by hand.
//
//	DATABASE_URL=... ENGINE_URL=http://matching-engine:8083 INTERNAL_TOKEN=... reconcile
//
// Exit status 0 means the report was produced and nothing needs attention.
// Exit status 2 means the report was produced and at least one row needs a
// human. Exit status 1 means the report could not be completed.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	jsonOut := flag.Bool("json", false, "write the report as JSON")
	flag.Parse()
	log.SetFlags(0)

	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	engineURL := strings.TrimRight(strings.TrimSpace(env("ENGINE_URL", "http://localhost:8083")), "/")
	token := os.Getenv("INTERNAL_TOKEN")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	rep, err := build(ctx, db, engineURL, token)
	if err != nil {
		log.Fatalf("reconcile: %v", err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			log.Fatalf("encode: %v", err)
		}
	} else {
		writeText(os.Stdout, rep)
	}
	if rep.Actionable > 0 {
		os.Exit(2)
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Report is the full read-only result.
type Report struct {
	GeneratedAt string    `json:"generatedAt"`
	EngineURL   string    `json:"engineUrl"`
	ReadOnly    bool      `json:"readOnly"`
	Actionable  int       `json:"actionable"`
	Symbols     []Symbol  `json:"symbols"`
	Failures    []Failure `json:"settlementFailures"`
	Errors      []string  `json:"errors,omitempty"`
}

// Symbol is one market's comparison.
type Symbol struct {
	Symbol   string    `json:"symbol"`
	Engine   int       `json:"engineResting"`
	DBOpen   int       `json:"dbOpen"`
	Findings []Finding `json:"findings"`
}

// Finding is one order that needs a look, or an in-flight order still waiting
// for its first engine report.
type Finding struct {
	Kind       string `json:"kind"`
	OrderID    string `json:"orderId"`
	UserID     string `json:"userId,omitempty"`
	Status     string `json:"status,omitempty"`
	Detail     string `json:"detail"`
	Actionable bool   `json:"actionable"`
}

// Failure is one unresolved settlement_failures row.
type Failure struct {
	ID        int64     `json:"id"`
	TradeID   string    `json:"tradeId"`
	OrderID   string    `json:"orderId"`
	UserID    string    `json:"userId"`
	Symbol    string    `json:"symbol"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

type dbOrder struct {
	id, user, symbol, side, status string
	seen                           bool
	deadline                       int64
}

type engOrder struct {
	OrderID   string `json:"orderId"`
	UserID    string `json:"userId"`
	Side      string `json:"side"`
	Price     string `json:"price"`
	Remaining string `json:"remaining"`
}

func build(ctx context.Context, db *sql.DB, engineURL, token string) (*Report, error) {
	rep := &Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		EngineURL:   engineURL,
		ReadOnly:    true,
	}
	open, err := loadOpen(ctx, db)
	if err != nil {
		return nil, err
	}
	fails, err := loadFailures(ctx, db)
	if err != nil {
		return nil, err
	}
	rep.Failures = fails
	rep.Actionable += len(fails)

	symbols := map[string]struct{}{}
	bySym := map[string][]dbOrder{}
	for _, o := range open {
		symbols[o.symbol] = struct{}{}
		bySym[o.symbol] = append(bySym[o.symbol], o)
	}
	rows, err := db.QueryContext(ctx, `SELECT symbol FROM symbols ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			symbols[s] = struct{}{}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(symbols))
	for s := range symbols {
		names = append(names, s)
	}
	sort.Strings(names)
	nowMs := time.Now().UnixMilli()
	client := &http.Client{Timeout: 10 * time.Second}

	for _, sym := range names {
		sec := Symbol{Symbol: sym, DBOpen: len(bySym[sym])}
		resting, err := fetchEngine(ctx, client, engineURL, token, sym)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", sym, err))
			rep.Symbols = append(rep.Symbols, sec)
			continue
		}
		sec.Engine = len(resting)
		engSet := map[string]engOrder{}
		for _, e := range resting {
			engSet[e.OrderID] = e
		}
		dbSet := map[string]dbOrder{}
		for _, o := range bySym[sym] {
			dbSet[o.id] = o
			if _, ok := engSet[o.id]; ok {
				continue
			}
			f := Finding{Kind: "db_open_missing_from_engine", OrderID: o.id, UserID: o.user, Status: o.status}
			switch {
			case !o.seen && o.deadline > 0 && o.deadline < nowMs:
				f.Detail = "open in the database, never seen by the engine, and its engine deadline has passed"
				f.Actionable = true
			case !o.seen:
				f.Kind = "awaiting_engine"
				f.Detail = "frozen and not yet observed on the result stream"
			default:
				f.Detail = "open in the database but not resting in the engine; a fill or cancel may be unapplied"
				f.Actionable = true
			}
			sec.Findings = append(sec.Findings, f)
			if f.Actionable {
				rep.Actionable++
			}
		}
		for id, e := range engSet {
			if _, ok := dbSet[id]; ok {
				continue
			}
			st, user := lookupStatus(ctx, db, id)
			detail := "resting in the engine"
			if st == "" {
				detail += ", no database row"
			} else {
				detail += ", database status is " + st
			}
			sec.Findings = append(sec.Findings, Finding{
				Kind: "engine_resting_db_not_open", OrderID: id, UserID: firstNonEmpty(user, e.UserID),
				Status: st, Detail: detail, Actionable: true,
			})
			rep.Actionable++
		}
		sort.Slice(sec.Findings, func(i, j int) bool {
			if sec.Findings[i].Kind != sec.Findings[j].Kind {
				return sec.Findings[i].Kind < sec.Findings[j].Kind
			}
			return sec.Findings[i].OrderID < sec.Findings[j].OrderID
		})
		rep.Symbols = append(rep.Symbols, sec)
	}
	return rep, nil
}

func loadOpen(ctx context.Context, db *sql.DB) ([]dbOrder, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT order_id, user_id, symbol, side, status, engine_seen, COALESCE(engine_deadline, 0)
		FROM orders
		WHERE status IN ('new', 'partially_filled')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbOrder
	for rows.Next() {
		var o dbOrder
		if err := rows.Scan(&o.id, &o.user, &o.symbol, &o.side, &o.status, &o.seen, &o.deadline); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func loadFailures(ctx context.Context, db *sql.DB) ([]Failure, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, trade_id, order_id, user_id, symbol, reason, created_at
		FROM settlement_failures
		WHERE resolved_at IS NULL
		ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Failure
	for rows.Next() {
		var f Failure
		if err := rows.Scan(&f.ID, &f.TradeID, &f.OrderID, &f.UserID, &f.Symbol, &f.Reason, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func lookupStatus(ctx context.Context, db *sql.DB, orderID string) (status, user string) {
	_ = db.QueryRowContext(ctx, `SELECT status, user_id FROM orders WHERE order_id=$1`, orderID).Scan(&status, &user)
	return status, user
}

func fetchEngine(ctx context.Context, client *http.Client, base, token, symbol string) ([]engOrder, error) {
	u := base + "/engine/orders?symbol=" + url.QueryEscape(symbol)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("engine /engine/orders returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out []engOrder
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode engine orders: %w", err)
	}
	return out, nil
}

func writeText(w io.Writer, rep *Report) {
	fmt.Fprintf(w, "READ ONLY. No balances were changed. generated=%s engine=%s\n", rep.GeneratedAt, rep.EngineURL)
	fmt.Fprintf(w, "actionable=%d settlement_failures=%d\n", rep.Actionable, len(rep.Failures))
	for _, e := range rep.Errors {
		fmt.Fprintf(w, "ERROR %s\n", e)
	}
	for _, s := range rep.Symbols {
		if len(s.Findings) == 0 && s.Engine == 0 && s.DBOpen == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s  engine_resting=%d db_open=%d\n", s.Symbol, s.Engine, s.DBOpen)
		for _, f := range s.Findings {
			flag := "info"
			if f.Actionable {
				flag = "ACTION"
			}
			fmt.Fprintf(w, "  [%s] %s order=%s user=%s status=%s\n      %s\n", flag, f.Kind, f.OrderID, f.UserID, f.Status, f.Detail)
		}
	}
	if len(rep.Failures) > 0 {
		fmt.Fprintf(w, "\nunresolved settlement_failures\n")
		for _, f := range rep.Failures {
			fmt.Fprintf(w, "  [ACTION] id=%d trade=%s order=%s user=%s symbol=%s at=%s\n      %s\n",
				f.ID, f.TradeID, f.OrderID, f.UserID, f.Symbol, f.CreatedAt.UTC().Format(time.RFC3339), f.Reason)
		}
	}
	if rep.Actionable == 0 && len(rep.Errors) == 0 {
		fmt.Fprintf(w, "\nnothing to repair\n")
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
