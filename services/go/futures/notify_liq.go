package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// pendingLiqNotice is queued inside the liquidation/ADL transaction and only
// delivered after that transaction commits, so a rolled-back attempt never
// notifies. Delivery is best-effort via common.Notify (in-app + email per
// the user's trade-email preference). DedupKey is intentionally omitted:
// account's dedup insert path does not send email.
type pendingLiqNotice struct {
	acctUID string // futures account id; may be a copy: sub-account
	title   string
	body    string
}

func posDirLabel(p Position) string {
	if p.Side == "SHORT" || (p.Side != "LONG" && p.Qty.Sign() < 0) {
		return "空"
	}
	return "多"
}

func marginModeLabel(mode string) string {
	if mode == "isolated" {
		return "逐仓"
	}
	return "全仓"
}

func ownedContract(acctUID, symbol string) string {
	if strings.HasPrefix(acctUID, "copy:") {
		return "您跟随带单的 " + symbol
	}
	return "您的 " + symbol
}

func liqNoticeTitle() string { return "合约仓位已强平" }

func liqNoticeBody(acctUID string, p Position, mark, fee, shortfall decimal.Decimal) string {
	body := fmt.Sprintf("%s %s%s仓（数量 %s，标记价格 %s，杠杆 %dx）已被强制平仓。强平手续费 %s USDT。",
		ownedContract(acctUID, p.Symbol), marginModeLabel(p.Mode), posDirLabel(p),
		money(p.Qty.Abs()), money(mark), p.Leverage, money(fee))
	if shortfall.Sign() > 0 {
		body += fmt.Sprintf("穿仓亏损 %s USDT 已由保险基金弥补。", money(shortfall))
	}
	return body
}

func adlNoticeTitle() string { return "合约仓位已自动减仓" }

func adlNoticeBody(acctUID string, p Position, qty, px decimal.Decimal) string {
	return fmt.Sprintf("因对手方仓位强平且保险基金不足，%s %s%s仓已被自动减仓 %s，成交价格 %s。",
		ownedContract(acctUID, p.Symbol), marginModeLabel(p.Mode), posDirLabel(p),
		money(qty.Abs()), money(px))
}

func (s *Server) queueLiqNotice(acctUID string, p Position, mark, fee, shortfall decimal.Decimal) {
	if acctUID == "" || strings.HasPrefix(acctUID, "system:") {
		return
	}
	p.Symbol = strings.TrimSpace(p.Symbol)
	s.pendingNotices = append(s.pendingNotices, pendingLiqNotice{
		acctUID: acctUID,
		title:   liqNoticeTitle(),
		body:    liqNoticeBody(acctUID, p, mark, fee, shortfall),
	})
}

func (s *Server) queueADLNotice(counter Position, qty, px decimal.Decimal) {
	if counter.UserID == "" || strings.HasPrefix(counter.UserID, "system:") {
		return
	}
	s.pendingNotices = append(s.pendingNotices, pendingLiqNotice{
		acctUID: counter.UserID,
		title:   adlNoticeTitle(),
		body:    adlNoticeBody(counter.UserID, counter, qty, px),
	})
}

// commitLiq runs fn in a transaction. Notices queued during fn are delivered
// only if the transaction commits. The pending list is cleared at the start
// of every attempt so a deadlock retry cannot double-send.
func (s *Server) commitLiq(ctx context.Context, fn func(*sql.Tx) error) error {
	err := s.txLocked(ctx, func(tx *sql.Tx) error {
		s.pendingNotices = nil
		return fn(tx)
	})
	if err != nil {
		s.pendingNotices = nil
		return err
	}
	s.flushPendingNotices(ctx)
	return nil
}

func (s *Server) flushPendingNotices(ctx context.Context) {
	evs := s.pendingNotices
	s.pendingNotices = nil
	for _, ev := range evs {
		uid := s.resolveNotifyUID(ctx, ev.acctUID)
		if uid == "" {
			continue
		}
		s.emitNotice(uid, ev.title, ev.body)
	}
}

func (s *Server) emitNotice(uid, title, body string) {
	if uid == "" {
		return
	}
	if s.notify != nil {
		s.notify(uid, title, body)
		return
	}
	common.Notify(common.Notice{UID: uid, Kind: common.NoticeTrade, Title: title, Body: body})
}

// resolveNotifyUID maps a futures account id to the user who should receive
// the inbox/email notice. Copy sub-accounts (copy:<followID>) resolve to the
// follower; system accounts are skipped.
func (s *Server) resolveNotifyUID(ctx context.Context, uid string) string {
	if uid == "" || strings.HasPrefix(uid, "system:") {
		return ""
	}
	rest, ok := strings.CutPrefix(uid, "copy:")
	if !ok {
		return uid
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		log.Printf("futures: liq notify: bad copy account %q", uid)
		return ""
	}
	if s.db == nil {
		return ""
	}
	var follower string
	if err := s.db.QueryRowContext(ctx, `SELECT follower FROM fut_copy_follows WHERE id=$1`, id).Scan(&follower); err != nil {
		log.Printf("futures: liq notify: copy follow %d: %v", id, err)
		return ""
	}
	return follower
}
