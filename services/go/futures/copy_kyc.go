package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// requireKYC fails closed: a lookup error or a level below the configured
// minimum (default 1) cannot apply as a trader or start a follow.
func (s *Server) requireKYC(ctx context.Context, uid string) error {
	need := s.cfg.CopyMinKYCLevel
	if need <= 0 {
		return nil
	}
	lvl, err := s.copyKYCLevel(ctx, uid)
	if err != nil {
		return bad("KYC_UNAVAILABLE", "暂时无法确认身份认证状态，请稍后再试")
	}
	if lvl < need {
		return bad("KYC_REQUIRED", fmt.Sprintf("跟单交易须完成身份认证（至少 %d 级）", need))
	}
	return nil
}

func (s *Server) copyKYCLevel(ctx context.Context, uid string) (int, error) {
	if s.kycLevel != nil {
		return s.kycLevel(ctx, uid)
	}
	base := strings.TrimRight(s.cfg.AccountURL, "/")
	if base == "" {
		return 0, fmt.Errorf("ACCOUNT_INTERNAL_URL not set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/internal/account/kyc-level?uid="+url.QueryEscape(uid), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("account kyc-level HTTP %d", resp.StatusCode)
	}
	var out struct {
		Level int `json:"level"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Level, nil
}

func parseInvitation(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if len(v) < 4 || len(v) > 32 {
		return "", bad("INVALID_INVITATION", "邀请码须为 4–32 位字母或数字")
	}
	for _, r := range v {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return "", bad("INVALID_INVITATION", "邀请码须为 4–32 位字母或数字")
		}
	}
	return v, nil
}

func parseLogoURL(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if len(v) > 512 {
		return "", bad("INVALID_LOGO", "头像地址过长")
	}
	low := strings.ToLower(v)
	if !strings.HasPrefix(low, "https://") && !strings.HasPrefix(low, "http://") {
		return "", bad("INVALID_LOGO", "头像地址须为 http 或 https 链接")
	}
	return v, nil
}

func parseSupportSymbols(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '|' }) {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if len(p) > 32 {
			return "", bad("INVALID_SYMBOLS", "跟单币种不合法")
		}
		for _, r := range p {
			if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
				return "", bad("INVALID_SYMBOLS", "跟单币种不合法")
			}
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) > 64 {
			return "", bad("INVALID_SYMBOLS", "跟单币种数量过多")
		}
	}
	return strings.Join(out, ","), nil
}

func symbolAllowed(list, sym string) bool {
	list = strings.TrimSpace(list)
	if list == "" {
		return true
	}
	sym = strings.ToUpper(sym)
	for _, p := range strings.Split(list, ",") {
		if strings.TrimSpace(p) == sym {
			return true
		}
	}
	return false
}

func (s *Server) notifyCopy(uid, title, body, dedup string) {
	if uid == "" {
		return
	}
	common.Notify(common.Notice{UID: uid, Kind: common.NoticeTrade, Title: title, Body: body, DedupKey: dedup})
}

// checkStopLoss marks follows whose sub-account equity has dropped by at least
// stop_loss_amount from principal. Caller holds s.mu.
func (s *Server) checkStopLoss(ctx context.Context) {
	fs, err := s.follows(ctx, `status='active' AND stop_loss_amount > 0`)
	if err != nil {
		return
	}
	for _, f := range fs {
		st, err := s.state(ctx, s.db, copyAcct(f.ID))
		if err != nil {
			continue
		}
		// Trigger when equity <= principal − stop_loss_amount (lost at least the cap).
		floor := f.Principal.Sub(f.StopLossAmount)
		if st.Equity.GreaterThan(floor) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE fut_copy_follows SET status='stopping' WHERE id=$1 AND status='active'`, f.ID); err != nil {
			continue
		}
		s.notifyCopy(f.Follower, "跟单已触发止损",
			fmt.Sprintf("跟随 %s 的跟单已达到设定的止损金额 %s USDT，系统已停止跟单并将结算剩余资金。", f.Trader, money(f.StopLossAmount)),
			fmt.Sprintf("copy-sl-%d", f.ID))
	}
}

func moneyStr(d decimal.Decimal) string { return money(d).String() }

func (s *Server) copyPositionViews(ctx context.Context, uid string) ([]map[string]any, error) {
	ps, err := openPositions(ctx, s.db, uid)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, p := range ps {
		side := "long"
		if p.Qty.Sign() < 0 {
			side = "short"
		}
		mark, _ := s.mark(p.Symbol)
		upnl := dZero
		if mark.Sign() > 0 && p.Qty.Sign() != 0 {
			upnl = mark.Sub(p.Entry).Mul(p.Qty)
		}
		out = append(out, map[string]any{
			"symbol": p.Symbol, "side": side, "qty": p.Qty.Abs(), "signedQty": p.Qty,
			"entryPrice": p.Entry, "markPrice": mark, "unrealizedPnl": upnl,
			"leverage": p.Leverage, "marginMode": p.Mode,
			"tpPrice": p.TP, "slPrice": p.SL, "openedAt": p.UpdatedAt,
		})
	}
	return out, nil
}

// parseDealerIdentity validates the user-facing dealer identity. Only the two
// public values are accepted from traders; MECHANISM is reserved for internal
// assignment and is rejected here (mirrors the compat gateway behaviour).
func parseDealerIdentity(v string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "", "PUBLIC_AREA":
		return "PUBLIC_AREA", nil
	case "PRIVATE_AREA":
		return "PRIVATE_AREA", nil
	}
	return "", bad("INVALID_DEALER_IDENTITY", "带单身份仅支持公域或私域")
}
