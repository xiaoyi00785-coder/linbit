package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// Quote-currency preference: the one server-side source of truth for the fiat
// currency every client (header picker, settings page, popups) displays.

type preferencesResponse struct {
	QuoteCurrency string   `json:"quoteCurrency"`
	Default       string   `json:"defaultQuoteCurrency"`
	Supported     []string `json:"supportedQuoteCurrencies"`
}

func (s *Server) quoteCurrencies() []string {
	if len(s.cfg.QuoteCurrencies) > 0 {
		return s.cfg.QuoteCurrencies
	}
	return []string{"CNY", "USD"}
}

func (s *Server) defaultQuoteCurrency() string {
	if c := normQuoteCurrency(s.cfg.DefaultQuoteCurrency, s.quoteCurrencies()); c != "" {
		return c
	}
	return s.quoteCurrencies()[0]
}

// normQuoteCurrency upper-cases c and returns it if supported, else "".
func normQuoteCurrency(c string, supported []string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	for _, x := range supported {
		if x == c {
			return c
		}
	}
	return ""
}

// GetQuoteCurrency returns the stored preference ("" when unset).
func (st *Store) GetQuoteCurrency(ctx context.Context, userID int64) (string, error) {
	var v sql.NullString
	if err := st.db.QueryRowContext(ctx, `SELECT quote_currency FROM users WHERE id=$1`, userID).Scan(&v); err != nil {
		return "", err
	}
	return v.String, nil
}

// SetQuoteCurrency stores the preference.
func (st *Store) SetQuoteCurrency(ctx context.Context, userID int64, c string) error {
	_, err := st.db.ExecContext(ctx, `UPDATE users SET quote_currency=$2 WHERE id=$1`, userID, c)
	return err
}

// effectiveQuoteCurrency resolves the user's preference, falling back to the default.
func (s *Server) effectiveQuoteCurrency(ctx context.Context, userID int64) string {
	if c, err := s.store.GetQuoteCurrency(ctx, userID); err == nil {
		if n := normQuoteCurrency(c, s.quoteCurrencies()); n != "" {
			return n
		}
	}
	return s.defaultQuoteCurrency()
}

func (s *Server) prefsResp(ctx context.Context, userID int64) preferencesResponse {
	return preferencesResponse{QuoteCurrency: s.effectiveQuoteCurrency(ctx, userID), Default: s.defaultQuoteCurrency(), Supported: s.quoteCurrencies()}
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	common.WriteJSON(w, http.StatusOK, s.prefsResp(r.Context(), u.ID))
}

func (s *Server) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req struct {
		QuoteCurrency *string `json:"quoteCurrency"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || req.QuoteCurrency == nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "未提供要修改的字段")
		return
	}
	c := normQuoteCurrency(*req.QuoteCurrency, s.quoteCurrencies())
	if c == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "不支持的计价货币")
		return
	}
	if err := s.store.SetQuoteCurrency(r.Context(), u.ID, c); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "保存失败")
		return
	}
	common.WriteJSON(w, http.StatusOK, s.prefsResp(r.Context(), u.ID))
}
