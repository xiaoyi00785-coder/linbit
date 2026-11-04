package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// subaccounts.go: parent-managed sub-accounts.
//
// Rules (defaults, admin-configurable via sub_account_settings):
//   - at most MaxPerParent (20) sub-accounts per parent; subs cannot nest;
//   - the parent must have 2FA (TOTP) enabled to create sub-accounts;
//   - subs cannot deposit or withdraw; their only route out is a transfer back
//     to the parent (gateway fences /wallet, /account/withdraw-*; account's
//     internal withdraw verify also refuses subs);
//   - parent <-> sub spot transfers are idempotent on (parent, clientId);
//   - sub login is disabled by default; when enabled it uses the sub's own
//     password + TOTP (secret minted for the parent to hand over);
//   - API keys: read / spot_trade / futures_trade only (never withdraw), with
//     an optional IP allowlist; verified by the gateway via /internal/api-keys/verify.

const subEmailDomain = "sub.linbit.invalid" // RFC 2606 reserved: never deliverable

var subUsernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{2,19}$`)

// Allowed API-key permissions. "withdraw" is deliberately absent.
var subKeyPerms = map[string]bool{"read": true, "spot_trade": true, "futures_trade": true}

func subLoginEmail(parentUID, username string) string {
	return parentUID + "_" + username + "@" + subEmailDomain
}

func validSubUsername(u string) bool { return subUsernameRe.MatchString(u) }

// normalizePerms validates and de-duplicates permissions; read is implied.
func normalizePerms(in []string) ([]string, error) {
	seen := map[string]bool{"read": true}
	out := []string{"read"}
	for _, p := range in {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		if !subKeyPerms[p] {
			return nil, fmt.Errorf("permission %q not allowed", p)
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, nil
}

// normalizeIPs validates an IP / CIDR allowlist (max 20 entries).
func normalizeIPs(in []string) ([]string, error) {
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			if _, _, err := net.ParseCIDR(s); err != nil {
				return nil, fmt.Errorf("bad cidr %q", s)
			}
		} else if net.ParseIP(s) == nil {
			return nil, fmt.Errorf("bad ip %q", s)
		}
		out = append(out, s)
	}
	if len(out) > 20 {
		return nil, errors.New("too many ip entries")
	}
	return out, nil
}

// ipAllowed reports whether ip is permitted (empty allowlist = any).
func ipAllowed(list []string, ip string) bool {
	if len(list) == 0 {
		return true
	}
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		return false
	}
	for _, e := range list {
		if strings.Contains(e, "/") {
			if _, n, err := net.ParseCIDR(e); err == nil && n.Contains(addr) {
				return true
			}
		} else if ip2 := net.ParseIP(e); ip2 != nil && ip2.Equal(addr) {
			return true
		}
	}
	return false
}

func sha256Hex(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---- DTOs ----

type subDTO struct {
	UID          string    `json:"uid"`
	ParentUID    string    `json:"parentUid"`
	Username     string    `json:"username"`
	LoginEmail   string    `json:"loginEmail"`
	Nickname     string    `json:"nickname"`
	Note         string    `json:"note"`
	Status       string    `json:"status"`
	LoginEnabled bool      `json:"loginEnabled"`
	TOTPBound    bool      `json:"totpBound"`
	CreatedAt    time.Time `json:"createdAt"`
}

func toSubDTO(a *SubAccount) subDTO {
	return subDTO{UID: a.UID, ParentUID: a.ParentUID, Username: a.Username, LoginEmail: a.LoginEmail, Nickname: a.Nickname,
		Note: a.Note, Status: a.Status, LoginEnabled: a.LoginEnabled, TOTPBound: a.TOTPEnabled, CreatedAt: a.CreatedAt}
}

type keyDTO struct {
	APIKey      string     `json:"apiKey"`
	Label       string     `json:"label"`
	Permissions []string   `json:"permissions"`
	IPAllowlist []string   `json:"ipAllowlist"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
	// Secret is only present in the create response (shown once).
	Secret string `json:"secret,omitempty"`
}

func toKeyDTO(k *SubAPIKey) keyDTO {
	d := keyDTO{APIKey: k.APIKey, Label: k.Label, Permissions: k.Permissions, IPAllowlist: k.IPAllowlist, CreatedAt: k.CreatedAt}
	if k.LastUsedAt.Valid {
		t := k.LastUsedAt.Time
		d.LastUsedAt = &t
	}
	return d
}

type xferDTO struct {
	TransferID   string    `json:"transferId"`
	ClientID     string    `json:"clientId"`
	SubAccountID string    `json:"subAccountId"`
	Direction    string    `json:"direction"`
	Asset        string    `json:"asset"`
	Amount       string    `json:"amount"`
	Status       string    `json:"status"`
	ErrorCode    string    `json:"errorCode,omitempty"`
	Initiator    string    `json:"initiator"`
	CreatedAt    time.Time `json:"createdAt"`
}

func toXferDTO(t *SubTransfer) xferDTO {
	amt := t.Amount
	if d, err := decimal.NewFromString(amt); err == nil {
		amt = d.String()
	}
	return xferDTO{TransferID: t.TransferID, ClientID: t.ClientID, SubAccountID: t.SubUID, Direction: t.Direction, Asset: t.Asset,
		Amount: amt, Status: t.Status, ErrorCode: t.ErrorCode, Initiator: t.Initiator, CreatedAt: t.CreatedAt}
}

// ---- helpers ----

// parentCaller returns the authenticated user, refusing sub-account callers
// (sub-accounts cannot have or manage sub-accounts).
func (s *Server) parentCaller(w http.ResponseWriter, r *http.Request) (*User, bool) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return nil, false
	}
	pid, err := s.store.ParentIDOf(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return nil, false
	}
	if pid != 0 {
		common.WriteError(w, http.StatusForbidden, "SUB_ACCOUNT_FORBIDDEN", "子账户无权执行此操作")
		return nil, false
	}
	return u, true
}

func (s *Server) loadOwnSub(w http.ResponseWriter, r *http.Request, parent *User) (*SubAccount, bool) {
	sub, err := s.store.SubOfParent(r.Context(), parent.ID, r.PathValue("id"))
	if errors.Is(err, ErrSubNotFound) {
		common.WriteError(w, http.StatusNotFound, "SUB_ACCOUNT_NOT_FOUND", "子账户不存在")
		return nil, false
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return nil, false
	}
	return sub, true
}

func pageArgs(r *http.Request, defSize, maxSize int) (page, size int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	size, _ = strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > maxSize {
		size = defSize
	}
	return
}

// internalCall performs a service-to-service JSON request with the internal token.
func (s *Server) internalCall(ctx context.Context, method, base, path string, hdr map[string]string, body any) (int, []byte, error) {
	if base == "" {
		return 0, nil, errors.New("upstream not configured")
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, b, err
}

// ---- handlers: CRUD ----

func (s *Server) handleListSubs(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	page, size := pageArgs(r, 50, 100)
	subs, total, err := s.store.ListSubs(r.Context(), `sa.parent_id=$1`, []any{p.ID}, size, (page-1)*size)
	if err != nil {
		log.Printf("list subs: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	st, _ := s.store.SubSettings(r.Context())
	items := make([]subDTO, 0, len(subs))
	for i := range subs {
		items = append(items, toSubDTO(&subs[i]))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "size": size, "limit": st.MaxPerParent})
}

func (s *Server) handleCreateSub(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	var req struct {
		Username string `json:"username"`
		Nickname string `json:"nickname"`
		Note     string `json:"note"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if !validSubUsername(req.Username) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "用户名需为 3-20 位小写字母、数字或下划线，且以字母或数字开头")
		return
	}
	if len([]rune(req.Nickname)) > 32 || len([]rune(req.Note)) > 200 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "昵称最多 32 字，备注最多 200 字")
		return
	}
	st, err := s.store.SubSettings(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !st.Enabled {
		common.WriteError(w, http.StatusForbidden, "SUB_ACCOUNT_DISABLED", "子账户功能暂未开放")
		return
	}
	if st.RequireParent2FA && !p.TOTPEnabled {
		common.WriteError(w, http.StatusForbidden, "TWOFA_REQUIRED", "请先开启两步验证（谷歌验证器）")
		return
	}
	if p.Status == userFrozen {
		writeFrozen(w)
		return
	}
	// Random unusable password: login stays disabled until the parent sets one.
	pw, _ := randHex(24)
	hash, err := hashPassword(pw)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	nick := strings.TrimSpace(req.Nickname)
	if nick == "" {
		nick = req.Username
	}
	sub, err := s.store.CreateSub(r.Context(), p.ID, req.Username, subLoginEmail(p.UID, req.Username), nick, strings.TrimSpace(req.Note), hash, st.MaxPerParent)
	switch {
	case errors.Is(err, ErrSubLimit):
		common.WriteError(w, http.StatusConflict, "SUB_ACCOUNT_LIMIT", fmt.Sprintf("最多可创建 %d 个子账户", st.MaxPerParent))
		return
	case errors.Is(err, ErrSubUsername):
		common.WriteError(w, http.StatusConflict, "SUB_ACCOUNT_EXISTS", "该用户名已存在")
		return
	case err != nil:
		log.Printf("create sub: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.initAssetAccount(r.Context(), sub.UID)
	common.WriteJSON(w, http.StatusCreated, toSubDTO(sub))
}

func (s *Server) handleGetSub(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	sub, ok := s.loadOwnSub(w, r, p)
	if !ok {
		return
	}
	common.WriteJSON(w, http.StatusOK, toSubDTO(sub))
}

// PATCH /sub-accounts/{id} {nickname?, note?, loginEnabled?, password?, totpCode}
// Enabling login requires the parent's TOTP and a password (>= 8 chars); a
// fresh TOTP secret is minted for the sub and returned once.
func (s *Server) handlePatchSub(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	sub, ok := s.loadOwnSub(w, r, p)
	if !ok {
		return
	}
	var req struct {
		Nickname     *string `json:"nickname"`
		Note         *string `json:"note"`
		LoginEnabled *bool   `json:"loginEnabled"`
		Password     string  `json:"password"`
		TOTPCode     string  `json:"totpCode"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if (req.Nickname != nil && len([]rune(*req.Nickname)) > 32) || (req.Note != nil && len([]rune(*req.Note)) > 200) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "昵称最多 32 字，备注最多 200 字")
		return
	}
	var loginTOTP map[string]string
	if req.LoginEnabled != nil || req.Password != "" {
		if !requireParentTOTP(w, p, req.TOTPCode) {
			return
		}
	}
	if req.LoginEnabled != nil && *req.LoginEnabled && !sub.LoginEnabled {
		st, _ := s.store.SubSettings(r.Context())
		if !st.AllowSubLogin {
			common.WriteError(w, http.StatusForbidden, "SUB_LOGIN_NOT_ALLOWED", "平台暂未开放子账户登录")
			return
		}
		if !validLoginPassword(req.Password) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "开启登录需设置密码："+passwordRuleMsg)
			return
		}
		secret, uri, err := generateTOTP(sub.LoginEmail)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if err := s.store.SetTOTPSecret(r.Context(), sub.UID, secret); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if err := s.store.SetTOTPEnabled(r.Context(), sub.UID, true); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		loginTOTP = map[string]string{"secret": secret, "otpauthUri": uri}
	}
	if req.Password != "" {
		if !validLoginPassword(req.Password) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", passwordRuleMsg)
			return
		}
		hash, err := hashPassword(req.Password)
		if err != nil || s.store.UpdatePassword(r.Context(), sub.UserID, hash) != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	if err := s.store.UpdateSub(r.Context(), sub.UserID, req.Nickname, req.Note, req.LoginEnabled); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if req.LoginEnabled != nil && !*req.LoginEnabled {
		_ = s.store.RevokeAllRefreshTokens(r.Context(), sub.UserID)
	}
	sub, _ = s.store.SubByUserID(r.Context(), sub.UserID)
	out := map[string]any{"subAccount": toSubDTO(sub)}
	if loginTOTP != nil {
		out["loginTotp"] = loginTOTP
	}
	common.WriteJSON(w, http.StatusOK, out)
}

// POST /sub-accounts/{id}/freeze and /unfreeze {reason?}
func (s *Server) handleFreezeSub(freeze bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.parentCaller(w, r)
		if !ok {
			return
		}
		sub, ok := s.loadOwnSub(w, r, p)
		if !ok {
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		_ = common.DecodeJSON(r, &req)
		status := userActive
		if freeze {
			status = userFrozen
		}
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			reason = "parent:" + status
		}
		if err := s.store.SetSubStatus(r.Context(), sub.UserID, status, reason); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if freeze {
			_ = s.store.RevokeAllRefreshTokens(r.Context(), sub.UserID)
		}
		sub, _ = s.store.SubByUserID(r.Context(), sub.UserID)
		common.WriteJSON(w, http.StatusOK, toSubDTO(sub))
	}
}

// ---- transfers ----

// POST /sub-accounts/transfer {clientId, subAccountId, direction: to_sub|to_parent, asset, amount}
// A parent may move funds either way; a logged-in sub-account may only send
// its own funds back to its parent (subAccountId must be itself).
func (s *Server) handleSubTransfer(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req struct {
		ClientID     string `json:"clientId"`
		SubAccountID string `json:"subAccountId"`
		Direction    string `json:"direction"`
		Asset        string `json:"asset"`
		Amount       string `json:"amount"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	amount, aerr := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if req.ClientID == "" || len(req.ClientID) > 64 || req.Asset == "" || len(req.Asset) > 16 || aerr != nil || amount.Sign() <= 0 ||
		amount.Exponent() < -18 || (req.Direction != "to_sub" && req.Direction != "to_parent") {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	pid, err := s.store.ParentIDOf(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	var sub *SubAccount
	var parent *User
	initiator := "parent"
	if pid != 0 { // caller is a sub-account
		if req.Direction != "to_parent" || (req.SubAccountID != "" && req.SubAccountID != u.UID) {
			common.WriteError(w, http.StatusForbidden, "SUB_ACCOUNT_FORBIDDEN", "子账户只能向母账户划转")
			return
		}
		sub, err = s.store.SubByUserID(r.Context(), u.ID)
		if err == nil {
			parent, err = s.store.UserByID(r.Context(), pid)
		}
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		initiator = "sub"
	} else {
		parent = u
		sub, err = s.store.SubOfParent(r.Context(), u.ID, req.SubAccountID)
		if errors.Is(err, ErrSubNotFound) {
			common.WriteError(w, http.StatusNotFound, "SUB_ACCOUNT_NOT_FOUND", "子账户不存在")
			return
		}
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	if parent.Status == userFrozen {
		writeFrozen(w)
		return
	}
	// A frozen sub can still be emptied back to the parent, never funded.
	if sub.Status == userFrozen && (req.Direction == "to_sub" || initiator == "sub") {
		common.WriteError(w, http.StatusForbidden, "SUB_ACCOUNT_FROZEN", "子账户已冻结")
		return
	}
	tid, _ := randHex(12)
	cur, created, err := s.store.ClaimTransfer(r.Context(), SubTransfer{TransferID: "st_" + tid, ParentID: parent.ID, SubUserID: sub.UserID,
		ClientID: req.ClientID, Direction: req.Direction, Asset: req.Asset, Amount: amount.String(), Initiator: initiator})
	if err != nil {
		log.Printf("sub transfer claim: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !created {
		prev, _ := decimal.NewFromString(cur.Amount)
		if cur.SubUserID != sub.UserID || cur.Direction != req.Direction || cur.Asset != req.Asset || !prev.Equal(amount) {
			common.WriteError(w, http.StatusConflict, "CLIENT_ID_REUSED", "clientId 已用于另一笔划转")
			return
		}
		if cur.Status != "pending" {
			common.WriteJSON(w, http.StatusOK, toXferDTO(cur))
			return
		}
		// pending: retry the asset call (asset side is idempotent on ref).
	}
	from, to := parent.UID, sub.UID
	if req.Direction == "to_parent" {
		from, to = sub.UID, parent.UID
	}
	code, body, err := s.internalCall(r.Context(), http.MethodPost, s.cfg.AssetInternalURL, "/internal/user-transfer", nil, map[string]string{
		"userId": from, "toUserId": to, "account": "spot", "asset": req.Asset, "amount": amount.String(), "ref": "subxfer:" + cur.TransferID})
	if err != nil || code >= 500 {
		log.Printf("sub transfer asset call: status=%d err=%v", code, err)
		common.WriteError(w, http.StatusServiceUnavailable, "TRANSFER_PENDING", "划转处理中，请稍后用相同 clientId 重试")
		return
	}
	if code >= 400 {
		var e struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Code == "" {
			e.Code = "TRANSFER_FAILED"
		}
		_ = s.store.FinishTransfer(r.Context(), cur.TransferID, "failed", e.Code)
		cur.Status, cur.ErrorCode = "failed", e.Code
		if e.Code == "INSUFFICIENT_BALANCE" {
			common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
			return
		}
		common.WriteJSON(w, http.StatusOK, toXferDTO(cur))
		return
	}
	_ = s.store.FinishTransfer(r.Context(), cur.TransferID, "success", "")
	cur.Status = "success"
	common.WriteJSON(w, http.StatusOK, toXferDTO(cur))
}

// GET /sub-accounts/transfers?subAccountId=&page=&size=
func (s *Server) handleListSubTransfers(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	var subID int64
	if q := r.URL.Query().Get("subAccountId"); q != "" {
		sub, err := s.store.SubOfParent(r.Context(), p.ID, q)
		if err != nil {
			common.WriteError(w, http.StatusNotFound, "SUB_ACCOUNT_NOT_FOUND", "子账户不存在")
			return
		}
		subID = sub.UserID
	}
	page, size := pageArgs(r, 20, 100)
	list, total, err := s.store.ListTransfers(r.Context(), p.ID, subID, size, (page-1)*size)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]xferDTO, 0, len(list))
	for i := range list {
		items = append(items, toXferDTO(&list[i]))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "size": size})
}

// ---- visibility ----

type balanceItem struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
	Total  string `json:"total"`
}

func (s *Server) subBalances(ctx context.Context, uid string) ([]balanceItem, error) {
	code, body, err := s.internalCall(ctx, http.MethodGet, s.cfg.AssetInternalURL, "/internal/balances?userId="+url.QueryEscape(uid), nil, nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("asset status %d", code)
	}
	var out struct {
		Items []balanceItem `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	nz := []balanceItem{}
	for _, b := range out.Items {
		if t, err := decimal.NewFromString(b.Total); err == nil && t.Sign() == 0 {
			continue
		}
		nz = append(nz, b)
	}
	return nz, nil
}

// GET /sub-accounts/{id}/assets
func (s *Server) handleSubAssets(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	sub, ok := s.loadOwnSub(w, r, p)
	if !ok {
		return
	}
	bal, err := s.subBalances(r.Context(), sub.UID)
	if err != nil {
		log.Printf("sub assets: %v", err)
		common.WriteError(w, http.StatusBadGateway, "UPSTREAM_ERROR", "资产服务暂不可用")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"uid": sub.UID, "username": sub.Username, "balances": bal})
}

// GET /sub-accounts/assets — summary for all of the parent's sub-accounts.
func (s *Server) handleAllSubAssets(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	subs, _, err := s.store.ListSubs(r.Context(), `sa.parent_id=$1`, []any{p.ID}, 100, 0)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	type row struct {
		UID      string        `json:"uid"`
		Username string        `json:"username"`
		Nickname string        `json:"nickname"`
		Status   string        `json:"status"`
		Balances []balanceItem `json:"balances"`
	}
	totals := map[string]decimal.Decimal{}
	items := []row{}
	for _, sa := range subs {
		bal, err := s.subBalances(r.Context(), sa.UID)
		if err != nil {
			common.WriteError(w, http.StatusBadGateway, "UPSTREAM_ERROR", "资产服务暂不可用")
			return
		}
		for _, b := range bal {
			t, _ := decimal.NewFromString(b.Total)
			totals[b.Asset] = totals[b.Asset].Add(t)
		}
		items = append(items, row{UID: sa.UID, Username: sa.Username, Nickname: sa.Nickname, Status: sa.Status, Balances: bal})
	}
	tot := []map[string]string{}
	for a, v := range totals {
		tot = append(tot, map[string]string{"asset": a, "total": v.String()})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "totals": tot})
}

// GET /sub-accounts/{id}/orders?type=open|history|trades&symbol=&page=&size=
// Read-only proxy to the order service on behalf of the sub-account.
func (s *Server) handleSubOrders(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	sub, ok := s.loadOwnSub(w, r, p)
	if !ok {
		return
	}
	path := "/spot/orders/history"
	switch r.URL.Query().Get("type") {
	case "open":
		path = "/spot/orders/open"
	case "trades":
		path = "/spot/trades"
	}
	q := url.Values{}
	for _, k := range []string{"symbol", "page", "size", "pageSize", "status", "startTime", "endTime"} {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	code, body, err := s.internalCall(r.Context(), http.MethodGet, s.cfg.OrderInternalURL, path, map[string]string{"X-User-Id": sub.UID}, nil)
	if err != nil {
		log.Printf("sub orders: %v", err)
		common.WriteError(w, http.StatusBadGateway, "UPSTREAM_ERROR", "订单服务暂不可用")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// ---- API keys ----

func (s *Server) handleListSubKeys(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	sub, ok := s.loadOwnSub(w, r, p)
	if !ok {
		return
	}
	keys, err := s.store.ListSubKeys(r.Context(), sub.UserID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]keyDTO, 0, len(keys))
	for i := range keys {
		items = append(items, toKeyDTO(&keys[i]))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /sub-accounts/{id}/api-keys {label, permissions[], ipAllowlist[], totpCode}
func (s *Server) handleCreateSubKey(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	sub, ok := s.loadOwnSub(w, r, p)
	if !ok {
		return
	}
	var req struct {
		Label       string   `json:"label"`
		Permissions []string `json:"permissions"`
		IPAllowlist []string `json:"ipAllowlist"`
		TOTPCode    string   `json:"totpCode"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || len([]rune(req.Label)) > 40 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	perms, err := normalizePerms(req.Permissions)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "PERMISSION_NOT_ALLOWED", "仅支持 read / spot_trade / futures_trade，子账户 API 不可提现")
		return
	}
	ips, err := normalizeIPs(req.IPAllowlist)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "IP 白名单格式不正确（最多 20 条）")
		return
	}
	if !requireParentTOTP(w, p, req.TOTPCode) {
		return
	}
	if sub.Status == userFrozen {
		common.WriteError(w, http.StatusForbidden, "SUB_ACCOUNT_FROZEN", "子账户已冻结")
		return
	}
	st, _ := s.store.SubSettings(r.Context())
	if n, err := s.store.CountActiveSubKeys(r.Context(), sub.UserID); err != nil || n >= st.MaxAPIKeysPerSub {
		common.WriteError(w, http.StatusConflict, "API_KEY_LIMIT", fmt.Sprintf("每个子账户最多 %d 个 API Key", st.MaxAPIKeysPerSub))
		return
	}
	keyHex, err1 := randHex(16)
	secret, err2 := randHex(32)
	if err1 != nil || err2 != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	k, err := s.store.CreateSubKey(r.Context(), SubAPIKey{APIKey: "lbk_" + keyHex, SecretHash: sha256Hex(secret), SubUserID: sub.UserID,
		ParentID: p.ID, Label: strings.TrimSpace(req.Label), Permissions: perms, IPAllowlist: ips})
	if err != nil {
		log.Printf("create sub key: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	d := toKeyDTO(k)
	d.Secret = secret
	common.WriteJSON(w, http.StatusCreated, d)
}

func (s *Server) handleDeleteSubKey(w http.ResponseWriter, r *http.Request) {
	p, ok := s.parentCaller(w, r)
	if !ok {
		return
	}
	sub, ok := s.loadOwnSub(w, r, p)
	if !ok {
		return
	}
	done, err := s.store.RevokeSubKey(r.Context(), sub.UserID, r.PathValue("apiKey"))
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !done {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "API Key 不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// POST /internal/api-keys/verify {apiKey, apiSecret, ip} (gateway only)
func (s *Server) handleInternalVerifyKey(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	var req struct {
		APIKey    string `json:"apiKey"`
		APISecret string `json:"apiSecret"`
		IP        string `json:"ip"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || req.APIKey == "" || req.APISecret == "" {
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "invalid"})
		return
	}
	deny := func(reason string) { common.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": reason}) }
	k, err := s.store.SubKeyByAPIKey(r.Context(), req.APIKey)
	if err != nil || k.RevokedAt.Valid || subtle.ConstantTimeCompare([]byte(k.SecretHash), []byte(sha256Hex(req.APISecret))) != 1 {
		deny("invalid")
		return
	}
	if !ipAllowed(k.IPAllowlist, req.IP) {
		deny("ip_not_allowed")
		return
	}
	sub, err := s.store.SubByUserID(r.Context(), k.SubUserID)
	if err != nil {
		deny("invalid")
		return
	}
	parent, err := s.store.UserByID(r.Context(), sub.ParentID)
	if err != nil {
		deny("invalid")
		return
	}
	if sub.Status == userFrozen || parent.Status == userFrozen {
		deny("account_frozen")
		return
	}
	s.store.TouchSubKey(r.Context(), k.ID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "uid": sub.UID, "parentUid": sub.ParentUID, "permissions": k.Permissions})
}

// ---- internal admin ----

// GET /internal/admin/sub-accounts?parentUid=&q=&status=&page=&size=
func (s *Server) handleInternalAdminSubs(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	where := "TRUE"
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where += " AND " + strings.ReplaceAll(cond, "?", "$"+itoa(len(args)))
	}
	q := r.URL.Query()
	if v := q.Get("parentUid"); v != "" {
		add("p.uid = ?", v)
	}
	if v := q.Get("status"); v != "" {
		add("u.status = ?", v)
	}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		add("(u.uid = ? OR sa.username ILIKE '%' || ? || '%')", v)
		// both placeholders reference the same arg number
	}
	page, size := pageArgs(r, 20, 100)
	subs, total, err := s.store.ListSubs(r.Context(), where, args, size, (page-1)*size)
	if err != nil {
		log.Printf("admin subs: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]subDTO, 0, len(subs))
	for i := range subs {
		items = append(items, toSubDTO(&subs[i]))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "size": size})
}

// GET/PUT /internal/admin/sub-accounts/settings
func (s *Server) handleInternalSubSettings(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	if r.Method == http.MethodPut {
		var st SubSettings
		if err := common.DecodeJSON(r, &st); err != nil || st.MaxPerParent < 0 || st.MaxPerParent > 200 || st.MaxAPIKeysPerSub < 0 || st.MaxAPIKeysPerSub > 50 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "参数不合法（maxPerParent 0-200，maxApiKeysPerSub 0-50）")
			return
		}
		if err := s.store.PutSubSettings(r.Context(), st); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	st, err := s.store.SubSettings(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, st)
}

// ErrSubLoginDisabled: the sub-account's login is not enabled by its parent.
var ErrSubLoginDisabled = errors.New("sub-account login disabled")

func writeSubLoginDisabled(w http.ResponseWriter) {
	common.WriteError(w, http.StatusForbidden, "SUB_LOGIN_DISABLED", "该子账户未开启登录，请联系母账户")
}

// requireParentTOTP: sub-account management always needs the parent's TOTP.
// TWOFA_REQUIRED (403) = TOTP not bound or not supplied; INVALID_TOTP (401) = wrong code.
func requireParentTOTP(w http.ResponseWriter, p *User, code string) bool {
	if !p.TOTPEnabled {
		common.WriteError(w, http.StatusForbidden, "TWOFA_REQUIRED", "请先开启两步验证（谷歌验证器）")
		return false
	}
	return requireTOTPIfEnabled(w, p, code)
}
