package main

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Watchlist (自选):
//
//	GET    /account/favorites?market=spot|futures        list (both markets when omitted)
//	PUT    /account/favorites {market, symbols:[...]}     replace one market's list, in order
//	PUT    /account/favorites/{symbol}?market=            add one (idempotent, appended)
//	DELETE /account/favorites/{symbol}?market=            remove one (idempotent)

const maxFavorites = 200

var reFavSymbol = regexp.MustCompile(`^[A-Z0-9]{2,24}$`)

type Favorite struct {
	Symbol    string    `json:"symbol"`
	Market    string    `json:"market"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
}

func favMarket(m string) (string, bool) {
	m = strings.ToLower(strings.TrimSpace(m))
	if m == "" {
		return "spot", true
	}
	return m, m == "spot" || m == "futures"
}

// cleanFavSymbols upper-cases, validates and de-duplicates (first wins).
func cleanFavSymbols(in []string) ([]string, string) {
	if len(in) > maxFavorites {
		return nil, "自选最多 200 个"
	}
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.ToUpper(strings.TrimSpace(s))
		if !reFavSymbol.MatchString(s) {
			return nil, "交易对格式不合法: " + s
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out, ""
}

func (s *Store) Favorites(ctx context.Context, userID int64, market string) ([]Favorite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol, market, sort, created_at FROM user_favorites
		WHERE user_id=$1 AND ($2='' OR market=$2) ORDER BY market, sort, created_at`, userID, market)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Favorite{}
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.Symbol, &f.Market, &f.Sort, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) ReplaceFavorites(ctx context.Context, userID int64, market string, symbols []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_favorites WHERE user_id=$1 AND market=$2`, userID, market); err != nil {
		return err
	}
	for i, sym := range symbols {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_favorites (user_id, market, symbol, sort) VALUES ($1,$2,$3,$4)`, userID, market, sym, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddFavorite appends a symbol; returns false when the list is full.
func (s *Store) AddFavorite(ctx context.Context, userID int64, market, symbol string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO user_favorites (user_id, market, symbol, sort)
		SELECT $1, $2, $3, COALESCE(MAX(sort)+1, 0) FROM user_favorites WHERE user_id=$1 AND market=$2
		HAVING COUNT(*) < $4
		ON CONFLICT (user_id, market, symbol) DO NOTHING`, userID, market, symbol, maxFavorites)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return true, nil
	}
	var exists bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM user_favorites WHERE user_id=$1 AND market=$2 AND symbol=$3)`, userID, market, symbol).Scan(&exists)
	return exists, err
}

func (s *Store) RemoveFavorite(ctx context.Context, userID int64, market, symbol string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_favorites WHERE user_id=$1 AND market=$2 AND symbol=$3`, userID, market, symbol)
	return err
}

func (s *Server) favUser(w http.ResponseWriter, r *http.Request) (*User, bool) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
	}
	return u, ok
}

func (s *Server) writeFavorites(w http.ResponseWriter, r *http.Request, userID int64, market string) {
	items, err := s.store.Favorites(r.Context(), userID, market)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	syms := []string{}
	for _, f := range items {
		syms = append(syms, f.Symbol)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"market": market, "items": items, "symbols": syms})
}

func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	u, ok := s.favUser(w, r)
	if !ok {
		return
	}
	m := strings.ToLower(r.URL.Query().Get("market"))
	if m != "" && m != "spot" && m != "futures" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "market 必须为 spot/futures")
		return
	}
	s.writeFavorites(w, r, u.ID, m)
}

func (s *Server) handleReplaceFavorites(w http.ResponseWriter, r *http.Request) {
	u, ok := s.favUser(w, r)
	if !ok {
		return
	}
	var in struct {
		Market  string   `json:"market"`
		Symbols []string `json:"symbols"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Symbols == nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "symbols 必填（可为空数组）")
		return
	}
	m, ok := favMarket(in.Market)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "market 必须为 spot/futures")
		return
	}
	syms, msg := cleanFavSymbols(in.Symbols)
	if msg != "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", msg)
		return
	}
	if err := s.store.ReplaceFavorites(r.Context(), u.ID, m, syms); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.writeFavorites(w, r, u.ID, m)
}

func (s *Server) favOne(w http.ResponseWriter, r *http.Request) (*User, string, string, bool) {
	u, ok := s.favUser(w, r)
	if !ok {
		return nil, "", "", false
	}
	m, ok := favMarket(r.URL.Query().Get("market"))
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "market 必须为 spot/futures")
		return nil, "", "", false
	}
	syms, msg := cleanFavSymbols([]string{r.PathValue("symbol")})
	if msg != "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", msg)
		return nil, "", "", false
	}
	return u, m, syms[0], true
}

func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	u, m, sym, ok := s.favOne(w, r)
	if !ok {
		return
	}
	added, err := s.store.AddFavorite(r.Context(), u.ID, m, sym)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !added {
		common.WriteError(w, http.StatusBadRequest, "FAVORITES_LIMIT", "自选最多 200 个")
		return
	}
	s.writeFavorites(w, r, u.ID, m)
}

func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	u, m, sym, ok := s.favOne(w, r)
	if !ok {
		return
	}
	if err := s.store.RemoveFavorite(r.Context(), u.ID, m, sym); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.writeFavorites(w, r, u.ID, m)
}
