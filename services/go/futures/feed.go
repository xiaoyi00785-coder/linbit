package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// feed.go: GET /internal/trades/feed?after=<tradeId>&limit= — one row per
// side (maker, taker) of every futures fill, for the growth service
// (rebates, points, volume tasks). Copy sub-accounts are reported as their
// follower; platform accounts (system:*) are skipped. bonusFee is the part
// of the fee paid from a trial bonus (not rebate-eligible).

type feedItem struct {
	TradeID  int64  `json:"tradeId"`
	UserID   string `json:"userId"`
	Symbol   string `json:"symbol"`
	Role     string `json:"role"`
	Notional string `json:"notional"`
	Fee      string `json:"fee"`
	BonusFee string `json:"bonusFee"`
	Copy     bool   `json:"copy"`
	Time     int64  `json:"time"`
}

func (s *Server) tradeFeedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/trades/feed", s.internalOnly(s.hTradeFeed))
}

func (s *Server) hTradeFeed(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT t.id, t.symbol, t.price, t.qty, t.maker_user, t.taker_user, t.maker_fee, t.taker_fee,
		t.maker_bonus_fee, t.taker_bonus_fee, t.created_at,
		COALESCE(mf.follower,''), COALESCE(tf.follower,'')
		FROM fut_trades t
		LEFT JOIN fut_copy_follows mf ON t.maker_user = 'copy:' || mf.id
		LEFT JOIN fut_copy_follows tf ON t.taker_user = 'copy:' || tf.id
		WHERE t.id > $1 ORDER BY t.id LIMIT $2`, after, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []feedItem{}
	last := after
	for rows.Next() {
		var id int64
		var sym, mu, tu, mFollower, tFollower string
		var price, qty, mfee, tfee, mb, tb decimal.Decimal
		var at time.Time
		if err := rows.Scan(&id, &sym, &price, &qty, &mu, &tu, &mfee, &tfee, &mb, &tb, &at, &mFollower, &tFollower); err != nil {
			writeErr(w, err)
			return
		}
		last = id
		notional := price.Mul(qty)
		add := func(user, follower, role string, fee, bonus decimal.Decimal) {
			if strings.HasPrefix(user, "system:") {
				return
			}
			isCopy := strings.HasPrefix(user, "copy:")
			if isCopy {
				if follower == "" {
					return
				}
				user = follower
			}
			items = append(items, feedItem{TradeID: id, UserID: user, Symbol: sym, Role: role, Notional: notional.String(),
				Fee: fee.String(), BonusFee: bonus.String(), Copy: isCopy, Time: at.UnixMilli()})
		}
		add(mu, mFollower, "maker", mfee, mb)
		add(tu, tFollower, "taker", tfee, tb)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next": last})
}
