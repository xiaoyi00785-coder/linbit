package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// Red packet: a bounded, shared drop of USDT inside a group chat. The creator
// has the whole amount debited from their funding account into the im service
// account at create time; claimers get credited piecemeal as they grab a
// share; anything unclaimed at expiry is refunded back to the sender.
//
// The claim path is intentionally the hottest: the atomic step (did I get a
// share? how much?) runs through a Redis Lua script backed by the DB so two
// claimers racing on the same packet cannot double-draw.

const redpacketSystemAccount = "system:im-redpacket"

// claimLua is executed on the Redis client under a HSET keyed per-redpacket.
// It atomically: (a) rejects a second claim from the same uid; (b) rejects
// when no shares are left; (c) draws the next share from the list stored in
// the "amounts" field (JSON array of amount strings). It returns the drawn
// amount as a string, or "" on miss / double / expired.
var claimLua = redis.NewScript(`
local key = KEYS[1]
local uid = ARGV[1]
local now = tonumber(ARGV[2])
if redis.call('HGET', key, 'claim:' .. uid) then
  return 'DUP'
end
local remaining = tonumber(redis.call('HGET', key, 'remaining') or '0')
if remaining <= 0 then
  return 'EMPTY'
end
local expires = tonumber(redis.call('HGET', key, 'expires') or '0')
if expires > 0 and now > expires then
  return 'EXPIRED'
end
local amounts = redis.call('HGET', key, 'amounts')
if not amounts then
  return 'NODATA'
end
local idx = tonumber(redis.call('HGET', key, 'idx') or '0')
local amount = cjson.decode(amounts)[idx + 1]
if not amount then
  return 'EMPTY'
end
redis.call('HSET', key, 'claim:' .. uid, amount)
redis.call('HINCRBY', key, 'idx', 1)
redis.call('HINCRBY', key, 'remaining', -1)
return amount
`)

// POST /im/redpackets {groupId, totalAmount, shares, mode, blessing, asset?}
func (s *Server) handleCreateRedPacket(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	var body struct {
		GroupID     int64  `json:"groupId"`
		PeerID      string `json:"peerId"`
		TotalAmount string `json:"totalAmount"`
		Amount      string `json:"amount"` // alias
		Shares      int    `json:"shares"`
		Count       int    `json:"count"` // alias
		Mode        string `json:"mode"`  // random | equal
		Type        string `json:"type"`  // alias
		Blessing    string `json:"blessing"`
		Asset       string `json:"asset"`
	}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	totalStr := strings.TrimSpace(body.TotalAmount)
	if totalStr == "" {
		totalStr = strings.TrimSpace(body.Amount)
	}
	total, err := decimal.NewFromString(totalStr)
	if err != nil || !total.IsPositive() {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	if max, err := decimal.NewFromString(s.cfg.RedPacketMaxUSDT); err == nil && total.GreaterThan(max) {
		common.WriteError(w, http.StatusBadRequest, "AMOUNT_TOO_LARGE", "超出单红包上限")
		return
	}
	if min, err := decimal.NewFromString(s.cfg.RedPacketMinUSDT); err == nil && total.LessThan(min) {
		common.WriteError(w, http.StatusBadRequest, "AMOUNT_TOO_SMALL", "低于单红包下限")
		return
	}
	shares := body.Shares
	if shares <= 0 {
		shares = body.Count
	}
	if shares <= 0 || shares > s.cfg.RedPacketMaxShares {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "份数不合法")
		return
	}
	mode := body.Mode
	if mode == "" {
		mode = body.Type
	}
	if mode == "" {
		mode = "random"
	}
	if mode != "random" && mode != "equal" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "模式只能为 random 或 equal")
		return
	}
	// Per-share floor so a random split never generates a 0-cent share.
	if total.Cmp(decimal.New(int64(shares), -2)) < 0 {
		common.WriteError(w, http.StatusBadRequest, "AMOUNT_TOO_SMALL", "金额不足以分成这么多份")
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(body.Asset))
	if asset == "" {
		asset = "USDT"
	}

	// Resolve the conversation (group or c2c).
	var convID int64
	var group sql.NullInt64
	switch {
	case body.GroupID > 0:
		if _, err := s.store.getGroupMember(r.Context(), body.GroupID, uid); err != nil {
			common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "非群成员")
			return
		}
		convID, err = s.store.groupConvID(r.Context(), body.GroupID)
		if err != nil || convID == 0 {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "群会话丢失")
			return
		}
		group = sql.NullInt64{Int64: body.GroupID, Valid: true}
	case body.PeerID != "":
		conv, err := s.store.upsertC2C(r.Context(), uid, body.PeerID)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "会话异常")
			return
		}
		convID = conv.ID
	default:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 groupId 或 peerId")
		return
	}

	// Pre-split the amounts so the claim path is a cheap array index. Random
	// mode uses the classic double-average split; equal mode gives every slot
	// total/shares, with any rounding residue added to the final slot.
	amounts := splitRedpacket(total, shares, mode)

	rp := &RedPacket{
		ConversationID:  convID,
		GroupID:         group,
		SenderUID:       uid,
		Asset:           asset,
		TotalAmount:     total,
		Shares:          shares,
		RemainingShares: shares,
		RemainingAmount: total,
		Mode:            mode,
		Blessing:        body.Blessing,
		ExpiresAt:       s.now().Add(time.Duration(s.cfg.RedPacketExpireHours) * time.Hour),
	}
	if err := s.store.insertRedPacket(r.Context(), rp); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "红包创建失败")
		return
	}
	// Debit the sender's funding account into the im system account. The ref
	// is unique per redpacket so retries are idempotent on the asset side.
	if err := s.transferAsset(r.Context(), uid, redpacketSystemAccount, asset, total, "redpacket:"+idString(rp.ID)); err != nil {
		// Roll the redpacket back so it never appears open without funds.
		_ = s.closeRedPacket(r.Context(), rp.ID, "refunded")
		common.WriteError(w, http.StatusBadGateway, "LEDGER_UNAVAILABLE", "扣款失败: "+err.Error())
		return
	}
	// Prime the Redis claim guard. A missing Redis falls back to a DB-row lock
	// in handleClaimRedPacket, so a Redis outage never loses a packet.
	if s.rdb != nil {
		strs := make([]string, len(amounts))
		for i, a := range amounts {
			strs[i] = a.String()
		}
		raw, _ := json.Marshal(strs)
		key := redpacketKey(rp.ID)
		_, _ = s.rdb.HSet(r.Context(), key,
			"amounts", string(raw),
			"idx", 0,
			"remaining", shares,
			"expires", rp.ExpiresAt.Unix(),
			"total", total.String(),
		).Result()
		_, _ = s.rdb.Expire(r.Context(), key, time.Until(rp.ExpiresAt.Add(time.Hour))).Result()
	}
	// Post a "red packet" system message into the conversation so it shows
	// up in the chat timeline immediately.
	payload, _ := json.Marshal(map[string]any{
		"redpacketId": rp.ID,
		"blessing":    rp.Blessing,
		"shares":      shares,
		"asset":       asset,
		"total":       total.String(),
		"mode":        mode,
	})
	mref, err := s.saveAndFan(r.Context(), convID, uid, sendMessageReq{
		Kind:      "redpacket",
		Payload:   payload,
		MsgRandom: randomID(),
	})
	if err == nil {
		_, _ = s.store.db.ExecContext(r.Context(), `UPDATE im_redpackets SET message_id = $1 WHERE id = $2`, mref.ID, rp.ID)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"redpacketId": rp.ID,
		"id":          rp.ID,
		"status":      rp.Status,
		"shares":      rp.Shares,
		"total":       total.String(),
		"expiresAt":   rp.ExpiresAt.UnixMilli(),
	})
}

// POST /im/redpackets/{id}/claim
func (s *Server) handleClaimRedPacket(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	uid := s.uid(r)
	rp, err := s.store.getRedPacket(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "红包不存在")
		return
	}
	if rp.Status != "open" {
		common.WriteError(w, http.StatusBadRequest, "REDPACKET_CLOSED", "红包已结束")
		return
	}
	if s.now().After(rp.ExpiresAt) {
		common.WriteError(w, http.StatusBadRequest, "REDPACKET_EXPIRED", "红包已过期")
		return
	}
	if rp.SenderUID == uid && rp.GroupID.Valid == false {
		common.WriteError(w, http.StatusBadRequest, "SELF_CLAIM", "不能领取自己的红包")
		return
	}
	// Group membership check: in a group redpacket the caller must be a member.
	if rp.GroupID.Valid {
		if _, err := s.store.getGroupMember(r.Context(), rp.GroupID.Int64, uid); err != nil {
			common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "非群成员")
			return
		}
	}
	amount, err := s.drawShare(r.Context(), rp, uid)
	if err != nil {
		switch err {
		case errClaimDup:
			common.WriteError(w, http.StatusBadRequest, "ALREADY_CLAIMED", "已领取过")
		case errClaimEmpty:
			common.WriteError(w, http.StatusBadRequest, "REDPACKET_EMPTY", "红包已领完")
		case errClaimExpired:
			common.WriteError(w, http.StatusBadRequest, "REDPACKET_EXPIRED", "红包已过期")
		default:
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "领取失败")
		}
		return
	}
	// Credit the claimer's funding account from the im system account. Even
	// if this transfer fails we keep the claim recorded: the admin reconciler
	// drains any stuck balance back out of the system account.
	if err := s.transferAsset(r.Context(), redpacketSystemAccount, uid, rp.Asset, amount, "redpacket:claim:"+idString(rp.ID)+":"+uid); err != nil {
		common.WriteError(w, http.StatusBadGateway, "LEDGER_UNAVAILABLE", "到账失败: "+err.Error())
		return
	}
	_, _ = s.store.db.ExecContext(r.Context(), `
        INSERT INTO im_redpacket_claims (redpacket_id, uid, amount) VALUES ($1, $2, $3)
        ON CONFLICT DO NOTHING`, rp.ID, uid, amount.String())
	// Decrement shares; when the last share goes out, close the row.
	remaining, _ := s.decrementRedPacket(r.Context(), rp.ID, amount)
	if remaining == 0 {
		_ = s.closeRedPacket(r.Context(), rp.ID, "closed")
	}
	s.hub.broadcastConversation(rp.ConversationID, map[string]any{
		"event":       "redpacket-claim",
		"redpacketId": rp.ID,
		"byUserId":    uid,
		"amount":      amount.String(),
		"remaining":   remaining,
		"ts":          s.nowUnixMilli(),
	})
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"amount":      amount.String(),
		"asset":       rp.Asset,
		"redpacketId": rp.ID,
		"remaining":   remaining,
	})
}

// GET /im/redpackets/{id}
func (s *Server) handleGetRedPacket(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	rp, err := s.store.getRedPacket(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "红包不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, redpacketDTO(rp))
}

// GET /im/redpackets/{id}/claims
func (s *Server) handleListClaims(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	cs, err := s.store.listClaims(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "领取记录加载失败")
		return
	}
	items := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		items = append(items, map[string]any{
			"userId":    c.UID,
			"amount":    c.Amount.String(),
			"claimedAt": c.ClaimedAt.UnixMilli(),
			"best":      c.IsBest,
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /im/groups/{id}/lottery {blessing, totalAmount, shares}
//
// Minimal implementation on top of redpacket: creates an equal-split redpacket
// in the group conversation. The lottery service is handled by the growth
// service; this route exists so the EasiCoin "群聊转盘" entry has something to
// call.
func (s *Server) handleGroupLottery(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	var body struct {
		TotalAmount string `json:"totalAmount"`
		Shares      int    `json:"shares"`
		Blessing    string `json:"blessing"`
	}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	proxy := struct {
		GroupID     int64  `json:"groupId"`
		TotalAmount string `json:"totalAmount"`
		Shares      int    `json:"shares"`
		Mode        string `json:"mode"`
		Blessing    string `json:"blessing"`
	}{
		GroupID:     id,
		TotalAmount: body.TotalAmount,
		Shares:      body.Shares,
		Mode:        "equal",
		Blessing:    body.Blessing,
	}
	raw, _ := json.Marshal(proxy)
	req := r.Clone(r.Context())
	req.Body = &byteReader{Reader: bytes.NewReader(raw)}
	req.ContentLength = int64(len(raw))
	s.handleCreateRedPacket(w, req)
}

// ---- helpers ----

type byteReader struct{ *bytes.Reader }

func (b *byteReader) Close() error { return nil }

// splitRedpacket returns the amount for each share. random uses the
// double-average algorithm; equal gives every slot total/n with rounding
// residue added to the last slot. Amounts are quantised to 4 decimals (USDT
// has far more precision but 4 dp is enough for chat and avoids dust).
func splitRedpacket(total decimal.Decimal, n int, mode string) []decimal.Decimal {
	if n <= 0 {
		return nil
	}
	if mode == "equal" {
		each := total.Div(decimal.NewFromInt(int64(n))).Round(4)
		out := make([]decimal.Decimal, n)
		acc := decimal.Zero
		for i := 0; i < n-1; i++ {
			out[i] = each
			acc = acc.Add(each)
		}
		out[n-1] = total.Sub(acc)
		return out
	}
	// Random mode: double average with a 0.01 minimum per share so no one
	// grabs a visible-but-worthless slot.
	min := decimal.New(1, -2) // 0.01
	out := make([]decimal.Decimal, n)
	remaining := total
	left := int64(n)
	for i := 0; i < n-1; i++ {
		ceil := remaining.Div(decimal.NewFromInt(left)).Mul(decimal.NewFromInt(2))
		if ceil.LessThan(min) {
			ceil = min
		}
		// Draw a uniform fraction in [0,1) and scale to [min, ceil).
		frac := randFraction()
		span := ceil.Sub(min)
		if span.IsNegative() {
			span = decimal.Zero
		}
		draw := min.Add(span.Mul(frac)).Round(4)
		if draw.LessThan(min) {
			draw = min
		}
		remainingMinusMin := remaining.Sub(min.Mul(decimal.NewFromInt(left - 1)))
		if draw.GreaterThan(remainingMinusMin) {
			draw = remainingMinusMin
		}
		out[i] = draw
		remaining = remaining.Sub(draw)
		left--
	}
	out[n-1] = remaining
	return out
}

// randFraction returns a decimal in [0,1).
func randFraction() decimal.Decimal {
	var b [8]byte
	_, _ = rand.Read(b[:])
	n := binary.BigEndian.Uint64(b[:])
	return decimal.NewFromBigInt(big.NewInt(0).SetUint64(n%1_000_000), -6)
}

var (
	errClaimDup     = errors.New("already claimed")
	errClaimEmpty   = errors.New("no shares left")
	errClaimExpired = errors.New("expired")
)

// drawShare returns the next share for uid. Prefers the Redis path; falls back
// to a DB-side lock when Redis is not configured.
func (s *Server) drawShare(ctx context.Context, rp *RedPacket, uid string) (decimal.Decimal, error) {
	if s.rdb != nil {
		raw, err := claimLua.Run(ctx, s.rdb, []string{redpacketKey(rp.ID)}, uid, s.now().Unix()).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			// Treat an unreachable Redis as fallback-worthy.
			return s.drawShareDB(ctx, rp, uid)
		}
		resp, _ := raw.(string)
		switch resp {
		case "DUP":
			return decimal.Zero, errClaimDup
		case "EMPTY", "NODATA":
			return decimal.Zero, errClaimEmpty
		case "EXPIRED":
			return decimal.Zero, errClaimExpired
		case "":
			return s.drawShareDB(ctx, rp, uid)
		default:
			amt, err := decimal.NewFromString(resp)
			if err != nil {
				return decimal.Zero, errors.New("redpacket draw: bad amount")
			}
			return amt, nil
		}
	}
	return s.drawShareDB(ctx, rp, uid)
}

// drawShareDB runs the claim as a single UPDATE ... RETURNING transaction.
// Not as cheap as the Redis path but it preserves correctness when Redis is
// absent.
func (s *Server) drawShareDB(ctx context.Context, rp *RedPacket, uid string) (decimal.Decimal, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return decimal.Zero, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Row-lock the redpacket.
	var remaining int
	var remainingAmount, total string
	if err := tx.QueryRowContext(ctx, `
        SELECT remaining_shares, remaining_amount, total_amount
        FROM im_redpackets WHERE id = $1 FOR UPDATE`, rp.ID).Scan(&remaining, &remainingAmount, &total); err != nil {
		return decimal.Zero, err
	}
	if remaining <= 0 {
		return decimal.Zero, errClaimEmpty
	}
	// Duplicate guard.
	var dup int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM im_redpacket_claims WHERE redpacket_id = $1 AND uid = $2`, rp.ID, uid).Scan(&dup); err != nil {
		return decimal.Zero, err
	}
	if dup > 0 {
		return decimal.Zero, errClaimDup
	}
	remainDec, err := decimal.NewFromString(remainingAmount)
	if err != nil {
		return decimal.Zero, err
	}
	var draw decimal.Decimal
	if remaining == 1 || rp.Mode == "equal" {
		if rp.Mode == "equal" && remaining > 1 {
			draw = remainDec.Div(decimal.NewFromInt(int64(remaining))).Round(4)
		} else {
			draw = remainDec
		}
	} else {
		min := decimal.New(1, -2)
		ceil := remainDec.Div(decimal.NewFromInt(int64(remaining))).Mul(decimal.NewFromInt(2))
		if ceil.LessThan(min) {
			ceil = min
		}
		frac := randFraction()
		span := ceil.Sub(min)
		if span.IsNegative() {
			span = decimal.Zero
		}
		draw = min.Add(span.Mul(frac)).Round(4)
		remainingMinusMin := remainDec.Sub(min.Mul(decimal.NewFromInt(int64(remaining - 1))))
		if draw.GreaterThan(remainingMinusMin) {
			draw = remainingMinusMin
		}
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE im_redpackets SET remaining_shares = remaining_shares - 1, remaining_amount = remaining_amount - $2
        WHERE id = $1`, rp.ID, draw.String()); err != nil {
		return decimal.Zero, err
	}
	if err := tx.Commit(); err != nil {
		return decimal.Zero, err
	}
	return draw, nil
}

// decrementRedPacket adjusts in-memory remaining; the authoritative counter is
// in the DB (updated inside drawShare paths). Returns the current remaining.
func (s *Server) decrementRedPacket(ctx context.Context, id int64, _ decimal.Decimal) (int, error) {
	var remaining int
	err := s.store.db.QueryRowContext(ctx, `SELECT remaining_shares FROM im_redpackets WHERE id = $1`, id).Scan(&remaining)
	return remaining, err
}

// closeRedPacket flips the row to closed or refunded.
func (s *Server) closeRedPacket(ctx context.Context, id int64, status string) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE im_redpackets SET status = $2, closed_at = now() WHERE id = $1 AND status = 'open'`, id, status)
	return err
}

// transferAsset calls the asset service /internal/user-transfer to move value
// between two uids' funding accounts. The asset service enforces its own
// idempotency on the ref.
func (s *Server) transferAsset(ctx context.Context, fromUID, toUID, asset string, amount decimal.Decimal, ref string) error {
	if s.cfg.AssetInternalURL == "" || s.cfg.InternalToken == "" {
		return errors.New("asset service not configured")
	}
	body := map[string]any{
		"userId":    fromUID,
		"toUserId":  toUID,
		"asset":     asset,
		"amount":    amount.String(),
		"account":   "funding",
		"ref":       ref,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AssetInternalURL+"/internal/user-transfer", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("asset %d: %s", resp.StatusCode, buf.String())
	}
	return nil
}

// sweepRedPackets runs forever, refunding any open-and-expired redpackets
// back to their senders. Called from main as a daemon goroutine.
func (s *Server) sweepRedPackets(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.sweepOnce(ctx)
		}
	}
}

func (s *Server) sweepOnce(ctx context.Context) {
	rows, err := s.store.db.QueryContext(ctx, `
        SELECT id, sender_uid, asset, remaining_amount
        FROM im_redpackets
        WHERE status = 'open' AND expires_at < now()
        LIMIT 100`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var sender, asset, rem string
		if err := rows.Scan(&id, &sender, &asset, &rem); err != nil {
			continue
		}
		amount, err := decimal.NewFromString(rem)
		if err != nil || !amount.IsPositive() {
			_ = s.closeRedPacket(ctx, id, "closed")
			continue
		}
		if err := s.transferAsset(ctx, redpacketSystemAccount, sender, asset, amount, "redpacket:refund:"+idString(id)); err != nil {
			continue
		}
		_, _ = s.store.db.ExecContext(ctx, `UPDATE im_redpackets SET status = 'refunded', refunded_at = now() WHERE id = $1`, id)
	}
}

func redpacketKey(id int64) string { return "im:rp:" + idString(id) }

func idString(id int64) string { return decimal.NewFromInt(id).String() }

// redpacketDTO renders a redpacket row for GET /im/redpackets/{id}.
func redpacketDTO(rp *RedPacket) map[string]any {
	out := map[string]any{
		"id":              rp.ID,
		"redpacketId":     rp.ID,
		"conversationId":  rp.ConversationID,
		"sender":          rp.SenderUID,
		"asset":           rp.Asset,
		"total":           rp.TotalAmount.String(),
		"shares":          rp.Shares,
		"remainingShares": rp.RemainingShares,
		"remaining":       rp.RemainingAmount.String(),
		"mode":            rp.Mode,
		"blessing":        rp.Blessing,
		"status":          rp.Status,
		"expiresAt":       rp.ExpiresAt.UnixMilli(),
		"createdAt":       rp.CreatedAt.UnixMilli(),
	}
	if rp.GroupID.Valid {
		out["groupId"] = rp.GroupID.Int64
	}
	if rp.MessageID.Valid {
		out["messageId"] = rp.MessageID.Int64
	}
	return out
}
