package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// telegram.go: bind, unbind and inspect the user's Telegram account.
//
//   POST /account/bind-telegram   verify a TG Login Widget payload and store
//                                 telegram_id/username on the user row
//   POST /account/unbind-telegram clear the binding
//   GET  /account/telegram        current state
//
// TELEGRAM_BOT_TOKEN is the bot whose Login Widget the client uses. When it is
// empty every write returns TELEGRAM_UNAVAILABLE; the read endpoint still works
// (so the UI can show the current state).

// maxTelegramAuthAge rejects a Login Widget payload older than this. Telegram
// recommends ~1 day for an auth check; we keep a day to match their examples.
const maxTelegramAuthAge = 24 * time.Hour

type telegramStatus struct {
	Bound     bool    `json:"bound"`
	Available bool    `json:"available"`
	ID        *int64  `json:"telegramId"`
	Username  *string `json:"telegramUsername"`
	BoundAt   *string `json:"boundAt"`
}

type bindTelegramReq struct {
	// All fields from the Telegram Login Widget. Only id is required.
	ID        int64  `json:"telegramId"`
	Username  string `json:"telegramUsername"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	PhotoURL  string `json:"photoUrl"`
	AuthDate  int64  `json:"authDate"`
	Hash      string `json:"authHash"`
}

// ---- store ----

// setTelegram binds a telegram id+username to a user. Returns ErrPhoneExists
// (reused sentinel) on a unique-index collision so the caller can map it to a
// 409; nil ids unbind the user.
var ErrTelegramIDTaken = errors.New("telegram id already bound to another account")

func (s *Store) setTelegram(ctx context.Context, userID int64, id int64, username string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET telegram_id = $2, telegram_username = $3, telegram_bound_at = $4
		 WHERE id = $1`, userID, id, username, now)
	if uniqueConstraint(err) != "" {
		return ErrTelegramIDTaken
	}
	return err
}

func (s *Store) clearTelegram(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET telegram_id = NULL, telegram_username = '', telegram_bound_at = NULL
		 WHERE id = $1`, userID)
	return err
}

func (s *Store) telegramInfo(ctx context.Context, userID int64) (id sql.NullInt64, username string, boundAt sql.NullTime, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT telegram_id, telegram_username, telegram_bound_at FROM users WHERE id = $1`,
		userID).Scan(&id, &username, &boundAt)
	return
}

// ---- handlers ----

// POST /account/bind-telegram
func (s *Server) handleBindTelegram(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	token := strings.TrimSpace(s.cfg.TelegramBotToken)
	if token == "" {
		common.WriteError(w, http.StatusServiceUnavailable, "TELEGRAM_UNAVAILABLE", "Telegram 绑定暂不可用")
		return
	}
	var req bindTelegramReq
	if err := common.DecodeJSON(r, &req); err != nil || req.ID == 0 || req.Hash == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if !verifyTelegramLogin(token, req, s.now(), maxTelegramAuthAge) {
		common.WriteError(w, http.StatusUnauthorized, "TELEGRAM_SIGNATURE_INVALID", "Telegram 签名校验失败")
		return
	}
	switch err := s.store.setTelegram(r.Context(), u.ID, req.ID, strings.TrimSpace(req.Username), s.now()); {
	case errors.Is(err, ErrTelegramIDTaken):
		common.WriteError(w, http.StatusConflict, "TELEGRAM_ALREADY_BOUND", "该 Telegram 账号已绑定其他 Linbit 账户")
		return
	case err != nil:
		log.Printf("bind-telegram: %v", err)
		internalErr(w)
		return
	}
	s.notifyAsync(u, common.NoticeSecurity, "已绑定 Telegram",
		fmt.Sprintf("您的 Linbit 账户已绑定 Telegram 账号 @%s。如非本人操作，请立即解绑并修改登录密码。", safeUsername(req.Username, req.ID)))
	writeTelegramStatus(w, http.StatusOK, s, u.ID, true)
}

// POST /account/unbind-telegram
func (s *Server) handleUnbindTelegram(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	// Unbind is always available (even when TELEGRAM_BOT_TOKEN is not set),
	// so a user can always sever a stale binding.
	if err := s.store.clearTelegram(r.Context(), u.ID); err != nil {
		internalErr(w)
		return
	}
	s.notifyAsync(u, common.NoticeSecurity, "已解绑 Telegram",
		"您的 Linbit 账户已解绑 Telegram。如非本人操作，请立即修改登录密码并联系客服。")
	writeTelegramStatus(w, http.StatusOK, s, u.ID, true)
}

// GET /account/telegram
func (s *Server) handleGetTelegram(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	writeTelegramStatus(w, http.StatusOK, s, u.ID, s.cfg.TelegramBotToken != "")
}

func writeTelegramStatus(w http.ResponseWriter, code int, s *Server, userID int64, available bool) {
	id, uname, boundAt, err := s.store.telegramInfo(context.Background(), userID)
	if err != nil {
		internalErr(w)
		return
	}
	resp := telegramStatus{Available: available}
	if id.Valid {
		v := id.Int64
		resp.ID = &v
		resp.Bound = true
		if uname != "" {
			u := uname
			resp.Username = &u
		}
		if boundAt.Valid {
			t := boundAt.Time.UTC().Format(time.RFC3339)
			resp.BoundAt = &t
		}
	}
	common.WriteJSON(w, code, resp)
}

// ---- signature verification ----

// verifyTelegramLogin re-implements the Telegram Login Widget signature check:
//
//   secret_key = SHA256(bot_token)
//   data_check_string = "\n".join(sorted "key=value" pairs, hash excluded)
//   hash              = HMAC_SHA256(data_check_string, secret_key)
//
// The auth_date must also be recent (maxAge). Returns true on success.
// Reference: https://core.telegram.org/widgets/login#checking-authorization
func verifyTelegramLogin(botToken string, req bindTelegramReq, now time.Time, maxAge time.Duration) bool {
	if req.Hash == "" || req.ID == 0 || req.AuthDate == 0 {
		return false
	}
	if maxAge > 0 {
		age := now.Sub(time.Unix(req.AuthDate, 0))
		if age < -5*time.Minute || age > maxAge {
			return false
		}
	}
	pairs := map[string]string{
		"id":        strconv.FormatInt(req.ID, 10),
		"auth_date": strconv.FormatInt(req.AuthDate, 10),
	}
	if req.Username != "" {
		pairs["username"] = req.Username
	}
	if req.FirstName != "" {
		pairs["first_name"] = req.FirstName
	}
	if req.LastName != "" {
		pairs["last_name"] = req.LastName
	}
	if req.PhotoURL != "" {
		pairs["photo_url"] = req.PhotoURL
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(pairs[k])
	}
	secret := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(b.String()))
	got := hex.EncodeToString(mac.Sum(nil))
	want, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(req.Hash)))
	if err != nil {
		return false
	}
	gotBytes, _ := hex.DecodeString(got)
	return hmac.Equal(gotBytes, want)
}

func safeUsername(name string, id int64) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "id" + strconv.FormatInt(id, 10)
}
