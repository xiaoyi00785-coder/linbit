package main

import (
	"context"
	"database/sql"
)

// SymbolInfo is a trading pair read from the shared `symbols` table (owned by
// the order service). chat-svc reads it to build per-spot chat rooms.
type SymbolInfo struct {
	Symbol string
	Base   string
	Quote  string
	Status string
}

// Store is chat-svc's database handle (shared Postgres).
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Symbols returns listed spot pairs ordered by symbol. Missing table => no rows.
func (s *Store) Symbols(ctx context.Context) ([]SymbolInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT symbol, base, quote, status FROM symbols ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SymbolInfo
	for rows.Next() {
		var si SymbolInfo
		if err := rows.Scan(&si.Symbol, &si.Base, &si.Quote, &si.Status); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}
