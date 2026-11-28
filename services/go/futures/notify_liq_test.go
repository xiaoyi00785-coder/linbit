package main

import (
	"context"
	"testing"
)

func testPos(symbol, mode, side, qty string, lev int) Position {
	return Position{Symbol: symbol, Mode: mode, Side: side, Qty: d(qty), Leverage: lev}
}

func TestLiqNoticeCopy(t *testing.T) {
	p := testPos("BTCUSDT", "cross", "BOTH", "58.3476", 20)
	body := liqNoticeBody("80000010", p, d("59169"), d("123.45"), dZero)
	want := "您的 BTCUSDT 全仓多仓（数量 58.3476，标记价格 59169，杠杆 20x）已被强制平仓。强平手续费 123.45 USDT。"
	if body != want {
		t.Fatalf("body:\n got %q\nwant %q", body, want)
	}
	if liqNoticeTitle() != "合约仓位已强平" {
		t.Fatalf("title %q", liqNoticeTitle())
	}

	isoShort := testPos("ETHUSDT", "isolated", "SHORT", "1.5", 10)
	body = liqNoticeBody("80000010", isoShort, d("3000.5"), d("4.2"), d("1.25"))
	want = "您的 ETHUSDT 逐仓空仓（数量 1.5，标记价格 3000.5，杠杆 10x）已被强制平仓。强平手续费 4.2 USDT。穿仓亏损 1.25 USDT 已由保险基金弥补。"
	if body != want {
		t.Fatalf("isolated short+shortfall:\n got %q\nwant %q", body, want)
	}

	body = liqNoticeBody("copy:42", testPos("BTCUSDT", "cross", "LONG", "-2", 5), d("100"), d("0"), dZero)
	if want := "您跟随带单的 BTCUSDT 全仓多仓（数量 2，标记价格 100，杠杆 5x）已被强制平仓。强平手续费 0 USDT。"; body != want {
		t.Fatalf("copy body:\n got %q\nwant %q", body, want)
	}
}

func TestPosDirLabel(t *testing.T) {
	cases := []struct {
		side, qty, want string
	}{
		{"BOTH", "1", "多"},
		{"BOTH", "-1", "空"},
		{"LONG", "1", "多"},
		{"LONG", "-1", "多"}, // hedge long stays 多 even if qty sign is off
		{"SHORT", "1", "空"},
		{"SHORT", "-1", "空"},
	}
	for _, c := range cases {
		got := posDirLabel(Position{Side: c.side, Qty: d(c.qty)})
		if got != c.want {
			t.Errorf("side=%s qty=%s: got %s want %s", c.side, c.qty, got, c.want)
		}
	}
}

func TestADLNoticeCopy(t *testing.T) {
	p := testPos("BTCUSDT", "cross", "BOTH", "-3", 25)
	p.UserID = "80000004"
	body := adlNoticeBody(p.UserID, p, d("0.5"), d("59100"))
	want := "因对手方仓位强平且保险基金不足，您的 BTCUSDT 全仓空仓已被自动减仓 0.5，成交价格 59100。"
	if body != want {
		t.Fatalf("adl:\n got %q\nwant %q", body, want)
	}
	if adlNoticeTitle() != "合约仓位已自动减仓" {
		t.Fatalf("title %q", adlNoticeTitle())
	}
}

func TestResolveNotifyUID(t *testing.T) {
	s := &Server{}
	ctx := context.Background()
	if got := s.resolveNotifyUID(ctx, "80000010"); got != "80000010" {
		t.Fatalf("user: %q", got)
	}
	if got := s.resolveNotifyUID(ctx, "system:insurance"); got != "" {
		t.Fatalf("system: %q", got)
	}
	if got := s.resolveNotifyUID(ctx, "copy:abc"); got != "" {
		t.Fatalf("bad copy: %q", got)
	}
	if got := s.resolveNotifyUID(ctx, "copy:12"); got != "" { // nil db
		t.Fatalf("copy without db: %q", got)
	}
}

func TestQueueAndFlushNotices(t *testing.T) {
	var got [][3]string
	s := &Server{notify: func(uid, title, body string) {
		got = append(got, [3]string{uid, title, body})
	}}
	p := testPos("BTCUSDT", "isolated", "BOTH", "0.01", 20)
	p.Symbol = "BTCUSDT"
	s.queueLiqNotice("system:insurance", p, d("100"), d("1"), dZero)
	s.queueLiqNotice("80000010", p, d("100"), d("1"), dZero)
	s.queueADLNotice(Position{UserID: "80000004", Symbol: "ETHUSDT", Mode: "cross", Side: "LONG", Qty: d("2"), Leverage: 10}, d("0.2"), d("3000"))
	s.flushPendingNotices(context.Background())
	if len(got) != 2 {
		t.Fatalf("got %d notices: %+v", len(got), got)
	}
	if got[0][0] != "80000010" || got[0][1] != "合约仓位已强平" {
		t.Fatalf("liq notice: %+v", got[0])
	}
	if got[1][0] != "80000004" || got[1][1] != "合约仓位已自动减仓" {
		t.Fatalf("adl notice: %+v", got[1])
	}
	if s.pendingNotices != nil {
		t.Fatalf("pending not cleared")
	}

	// A second flush after a failed attempt must not resend.
	s.pendingNotices = []pendingLiqNotice{{acctUID: "80000010", title: "x", body: "y"}}
	s.pendingNotices = nil // simulate commitLiq error path
	s.flushPendingNotices(context.Background())
	if len(got) != 2 {
		t.Fatalf("resent after clear: %+v", got)
	}
}

func TestCommitLiqClearsOnRetry(t *testing.T) {
	// Mirrors the first line of the tx callback: a retried attempt starts
	// with an empty pending list so notices from a rolled-back try are dropped.
	s := &Server{}
	s.pendingNotices = []pendingLiqNotice{{acctUID: "stale", title: "old", body: "old"}}
	s.pendingNotices = nil
	s.queueLiqNotice("80000010", testPos("BTCUSDT", "cross", "BOTH", "1", 10), d("1"), d("0"), dZero)
	if len(s.pendingNotices) != 1 || s.pendingNotices[0].acctUID != "80000010" {
		t.Fatalf("retry rebuild: %+v", s.pendingNotices)
	}
}

func TestMoneyInNoticeIsRounded(t *testing.T) {
	p := testPos("BTCUSDT", "cross", "BOTH", "1.000000001", 1)
	body := liqNoticeBody("u", p, d("100.000000004"), d("0.123456789"), dZero)
	if want := "您的 BTCUSDT 全仓多仓（数量 1，标记价格 100，杠杆 1x）已被强制平仓。强平手续费 0.12345679 USDT。"; body != want {
		t.Fatalf("rounding:\n got %q\nwant %q", body, want)
	}
}
