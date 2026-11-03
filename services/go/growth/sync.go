package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// sync.go pulls the cursor feeds of account (users), order (spot fills),
// futures (fills) and asset (wallet ops) into growth's own tables.

func (s *Server) cursor(ctx context.Context, name string) int64 {
	var pos int64
	_ = s.db.QueryRowContext(ctx, `SELECT pos FROM g_cursors WHERE name=$1`, name).Scan(&pos)
	return pos
}

func (s *Server) setCursor(ctx context.Context, tx *sql.Tx, name string, pos int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO g_cursors (name, pos) VALUES ($1,$2) ON CONFLICT (name) DO UPDATE SET pos=$2`, name, pos)
	return err
}

// Sync pulls every feed until caught up, then runs program hooks.
func (s *Server) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range []func(context.Context) (int, error){s.syncUsers, s.syncSpot, s.syncFutures, s.syncOps} {
		for i := 0; i < 50; i++ {
			n, err := f(ctx)
			if err != nil {
				return err
			}
			if n == 0 {
				break
			}
		}
	}
	return s.afterSyncLocked(ctx)
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode() string {
	b := make([]byte, 8)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		b[i] = codeAlphabet[n.Int64()]
	}
	return string(b)
}

func (s *Server) syncUsers(ctx context.Context) (int, error) {
	after := s.cursor(ctx, "users")
	var out struct {
		Items []struct {
			ID         int64  `json:"id"`
			UID        string `json:"uid"`
			InviteCode string `json:"inviteCode"`
			KYCLevel   int    `json:"kycLevel"`
			CreatedAt  int64  `json:"createdAt"`
		} `json:"items"`
	}
	if err := s.call(ctx, "GET", fmt.Sprintf("%s/internal/users/feed?after=%d&limit=500", s.cfg.AccountURL, after), nil, &out); err != nil {
		return 0, err
	}
	if len(out.Items) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, u := range out.Items {
		var inviter any
		if c := strings.ToUpper(strings.TrimSpace(u.InviteCode)); c != "" {
			var inv string
			if err := tx.QueryRowContext(ctx, `SELECT uid FROM g_users WHERE invite_code=$1`, c).Scan(&inv); err == nil && inv != u.UID {
				inviter = inv
			}
		}
		for try := 0; ; try++ {
			_, err := tx.ExecContext(ctx, `SAVEPOINT u`)
			if err != nil {
				return 0, err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO g_users (uid, invite_code, inviter, kyc_level, registered_at) VALUES ($1,$2,$3,$4,$5)
				ON CONFLICT (uid) DO NOTHING`, u.UID, newCode(), inviter, u.KYCLevel, time.UnixMilli(u.CreatedAt))
			if err == nil {
				break
			}
			if _, e := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT u`); e != nil || try > 5 {
				return 0, err
			}
		}
		after = u.ID
	}
	if err := s.setCursor(ctx, tx, "users", after); err != nil {
		return 0, err
	}
	return len(out.Items), tx.Commit()
}

func (s *Server) syncSpot(ctx context.Context) (int, error) {
	after := s.cursor(ctx, "spot")
	var out struct {
		Items []struct {
			ID       int64           `json:"id"`
			UserID   string          `json:"userId"`
			Base     string          `json:"base"`
			Quote    string          `json:"quote"`
			Price    decimal.Decimal `json:"price"`
			Quantity decimal.Decimal `json:"quantity"`
			Fee      decimal.Decimal `json:"fee"`
			FeeAsset string          `json:"feeAsset"`
			Time     int64           `json:"time"`
		} `json:"items"`
	}
	if err := s.call(ctx, "GET", fmt.Sprintf("%s/internal/trades/feed?after=%d&limit=500", s.cfg.OrderURL, after), nil, &out); err != nil {
		return 0, err
	}
	if len(out.Items) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, t := range out.Items {
		after = t.ID
		if !s.cfg.USDQuotes[strings.ToUpper(t.Quote)] || strings.HasPrefix(t.UserID, "system:") {
			continue // only USD-quoted pairs are valued (GROWTH_USD_QUOTES)
		}
		fee := t.Fee
		if strings.EqualFold(t.FeeAsset, t.Base) {
			fee = t.Fee.Mul(t.Price)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO g_fee_events (src, src_id, uid, product, notional, fee, at)
			VALUES ('spot',$1,$2,'spot',$3,$4,$5) ON CONFLICT DO NOTHING`,
			fmt.Sprint(t.ID), t.UserID, t.Price.Mul(t.Quantity).Round(8), fee.Round(12), time.UnixMilli(t.Time)); err != nil {
			return 0, err
		}
	}
	if err := s.setCursor(ctx, tx, "spot", after); err != nil {
		return 0, err
	}
	return len(out.Items), tx.Commit()
}

func (s *Server) syncFutures(ctx context.Context) (int, error) {
	after := s.cursor(ctx, "futures")
	var out struct {
		Items []struct {
			TradeID  int64           `json:"tradeId"`
			UserID   string          `json:"userId"`
			Role     string          `json:"role"`
			Notional decimal.Decimal `json:"notional"`
			Fee      decimal.Decimal `json:"fee"`
			BonusFee decimal.Decimal `json:"bonusFee"`
			Copy     bool            `json:"copy"`
			Time     int64           `json:"time"`
		} `json:"items"`
		Next int64 `json:"next"`
	}
	if err := s.call(ctx, "GET", fmt.Sprintf("%s/internal/trades/feed?after=%d&limit=500", s.cfg.FuturesURL, after), nil, &out); err != nil {
		return 0, err
	}
	if out.Next <= after {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, t := range out.Items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO g_fee_events (src, src_id, uid, product, notional, fee, bonus_fee, copy, at)
			VALUES ('futures',$1,$2,'futures',$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("%d:%s", t.TradeID, t.Role), t.UserID, t.Notional.Round(8), t.Fee.Round(12), t.BonusFee.Round(12), t.Copy, time.UnixMilli(t.Time)); err != nil {
			return 0, err
		}
	}
	if err := s.setCursor(ctx, tx, "futures", out.Next); err != nil {
		return 0, err
	}
	return 1, tx.Commit()
}

func (s *Server) syncOps(ctx context.Context) (int, error) {
	after := s.cursor(ctx, "ops")
	var out struct {
		Items []struct {
			Seq    int64           `json:"seq"`
			Ref    string          `json:"ref"`
			Op     string          `json:"op"`
			UserID string          `json:"userId"`
			Asset  string          `json:"asset"`
			Amount decimal.Decimal `json:"amount"`
			Time   int64           `json:"time"`
		} `json:"items"`
	}
	if err := s.call(ctx, "GET", fmt.Sprintf("%s/internal/ops/feed?after=%d&limit=500", s.cfg.AssetURL, after), nil, &out); err != nil {
		return 0, err
	}
	if len(out.Items) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, o := range out.Items {
		after = o.Seq
		if o.Op != "deposit" && o.Op != "futures_out" && o.Op != "futures_in" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO g_wallet_ops (seq, ref, op, uid, asset, amount, at) VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT DO NOTHING`, o.Seq, o.Ref, o.Op, o.UserID, o.Asset, o.Amount, time.UnixMilli(o.Time)); err != nil {
			return 0, err
		}
	}
	if err := s.setCursor(ctx, tx, "ops", after); err != nil {
		return 0, err
	}
	return len(out.Items), tx.Commit()
}

// refreshKYC updates the cached KYC level of the given users from account.
func (s *Server) refreshKYC(ctx context.Context, uids []string) {
	for len(uids) > 0 {
		n := len(uids)
		if n > 200 {
			n = 200
		}
		var out struct {
			Items []struct {
				UID      string `json:"uid"`
				KYCLevel int    `json:"kycLevel"`
			} `json:"items"`
		}
		if err := s.call(ctx, "GET", s.cfg.AccountURL+"/internal/users/profile?uid="+strings.Join(uids[:n], ","), nil, &out); err == nil {
			for _, u := range out.Items {
				_, _ = s.db.ExecContext(ctx, `UPDATE g_users SET kyc_level=$2 WHERE uid=$1`, u.UID, u.KYCLevel)
			}
		}
		uids = uids[n:]
	}
}

var errNoUser = errors.New("unknown user")

// ensureUser makes sure the caller exists in g_users (syncing if needed).
func (s *Server) ensureUser(ctx context.Context, uid string) error {
	var x int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM g_users WHERE uid=$1`, uid).Scan(&x); err == nil {
		return nil
	}
	if err := s.Sync(ctx); err != nil {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM g_users WHERE uid=$1`, uid).Scan(&x); err != nil {
		return notFound("USER_NOT_FOUND", "用户不存在")
	}
	return nil
}
