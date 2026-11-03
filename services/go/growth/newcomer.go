package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Newcomer gift package (福利中心 · 新人限时任务) and the invite KYC reward.
//
// Flow: KYC → enroll → complete tasks inside the window → claim → voucher in
// the coupon center → activate. Every task is claimable once per user; the
// trading-volume levels stack.

type NewcomerTask struct {
	Key    string          `json:"key"`
	Title  string          `json:"title"`
	Kind   string          `json:"kind"` // reward voucher: trial | deduction
	Amount decimal.Decimal `json:"amount"`
	Target decimal.Decimal `json:"target"` // volume target / min deposit (0 for bind)
	Type   string          `json:"type"`   // bind | first_deposit | volume
}

type NewcomerRules struct {
	Tasks    []NewcomerTask
	Days     int // GROWTH_NEWCOMER_DAYS (7): window after enrollment
	KYCLevel int // GROWTH_NEWCOMER_KYC_LEVEL (1)
}

const defaultNewcomerVolume = "10000:5,50000:10,100000:15,300000:30,500000:50,1000000:100"

func loadNewcomerRules() NewcomerRules {
	r := NewcomerRules{Days: common.EnvInt("GROWTH_NEWCOMER_DAYS", 7), KYCLevel: common.EnvInt("GROWTH_NEWCOMER_KYC_LEVEL", 1)}
	r.Tasks = append(r.Tasks,
		NewcomerTask{Key: "bind", Title: "绑定邮箱和手机号", Kind: "deduction", Amount: dec("GROWTH_NEWCOMER_BIND_REWARD", "5"), Type: "bind"},
		NewcomerTask{Key: "first_deposit", Title: "首充并转入合约账户", Kind: "trial", Amount: dec("GROWTH_NEWCOMER_DEPOSIT_REWARD", "5"),
			Target: dec("GROWTH_NEWCOMER_DEPOSIT_MIN", "50"), Type: "first_deposit"})
	tiers := parseVolumeTiers(common.Env("GROWTH_NEWCOMER_VOLUME_TIERS", defaultNewcomerVolume))
	if len(tiers) == 0 {
		tiers = parseVolumeTiers(defaultNewcomerVolume)
	}
	for i, t := range tiers {
		lv := "Lv." + itoa(i+1)
		r.Tasks = append(r.Tasks, NewcomerTask{Key: "volume_" + itoa(i+1), Title: "新手合约交易 " + lv, Kind: "trial",
			Amount: decimal.NewFromInt(int64(t.Points)), Target: t.Volume, Type: "volume"})
	}
	return r
}

func itoa(i int) string { return decimal.NewFromInt(int64(i)).String() }

func (r NewcomerRules) total() decimal.Decimal {
	t := decimal.Zero
	for _, x := range r.Tasks {
		t = t.Add(x.Amount)
	}
	return t
}

type profile struct {
	UID           string `json:"uid"`
	KYCLevel      int    `json:"kycLevel"`
	EmailVerified bool   `json:"emailVerified"`
	PhoneVerified bool   `json:"phoneVerified"`
}

func (s *Server) profiles(ctx context.Context, uids []string) (map[string]profile, error) {
	out := map[string]profile{}
	if len(uids) == 0 {
		return out, nil
	}
	var res struct {
		Items []profile `json:"items"`
	}
	if err := s.call(ctx, "GET", s.cfg.AccountURL+"/internal/users/profile?uid="+strings.Join(uids, ","), nil, &res); err != nil {
		return nil, err
	}
	for _, p := range res.Items {
		out[p.UID] = p
	}
	return out, nil
}

func (s *Server) newcomerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /rewards/newcomer", s.user(s.handleNewcomer))
	mux.HandleFunc("POST /rewards/newcomer/enroll", s.user(s.handleNewcomerEnroll))
	mux.HandleFunc("POST /rewards/newcomer/tasks/{key}/claim", s.user(s.handleNewcomerClaim))
	mux.HandleFunc("GET /activities", s.handleActivities)
	mux.HandleFunc("GET /home/newcomer", s.handleHomeNewcomer)
}

type taskView struct {
	NewcomerTask
	Progress string `json:"progress"`
	Status   string `json:"status"` // locked | in_progress | claimable | claimed | expired
	CouponID *int64 `json:"couponId,omitempty"`
}

// newcomerState evaluates every task for uid.
func (s *Server) newcomerState(ctx context.Context, uid string) (map[string]any, []*taskView, error) {
	prof, err := s.profiles(ctx, []string{uid})
	if err != nil {
		return nil, nil, err
	}
	p := prof[uid]
	kycOK := p.KYCLevel >= s.cfg.Newcomer.KYCLevel
	out := map[string]any{"kycRequired": s.cfg.Newcomer.KYCLevel, "kycOk": kycOK, "enrolled": false,
		"total": s.cfg.Newcomer.total().String(), "asset": s.cfg.PayoutAsset, "days": s.cfg.Newcomer.Days}
	var enrolled, deadline time.Time
	err = s.db.QueryRowContext(ctx, `SELECT enrolled_at, deadline FROM g_newcomer WHERE uid=$1`, uid).Scan(&enrolled, &deadline)
	isEnrolled := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	claims := map[string]int64{}
	rows, err := s.db.QueryContext(ctx, `SELECT task, coupon_id FROM g_newcomer_claims WHERE uid=$1`, uid)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var k string
		var id int64
		if rows.Scan(&k, &id) == nil {
			claims[k] = id
		}
	}
	rows.Close()
	var vol decimal.Decimal
	var depositDone bool
	if isEnrolled {
		out["enrolled"], out["enrolledAt"], out["deadline"] = true, enrolled.UnixMilli(), deadline.UnixMilli()
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(notional),0) FROM g_fee_events WHERE uid=$1 AND product='futures' AND at >= $2 AND at < $3`,
			uid, enrolled, deadline).Scan(&vol); err != nil {
			return nil, nil, err
		}
		quotes := []string{}
		for q := range s.cfg.USDQuotes {
			quotes = append(quotes, q)
		}
		var minDep decimal.Decimal
		for _, t := range s.cfg.Newcomer.Tasks {
			if t.Type == "first_deposit" {
				minDep = t.Target
			}
		}
		// A deposit >= the minimum inside the window, followed by a transfer (asset op
		// futures_out = spot -> futures)
		// into the futures account (also inside the window).
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM g_wallet_ops d WHERE d.uid=$1 AND d.op='deposit' AND d.asset = ANY($4)
			AND d.amount >= $5 AND d.at >= $2 AND d.at < $3 AND EXISTS (SELECT 1 FROM g_wallet_ops f WHERE f.uid=$1 AND f.op='futures_out' AND f.at >= d.at AND f.at < $3))`,
			uid, enrolled, deadline, quotes, minDep).Scan(&depositDone); err != nil {
			return nil, nil, err
		}
	}
	expired := isEnrolled && !s.now().Before(deadline)
	var tasks []*taskView
	for _, t := range s.cfg.Newcomer.Tasks {
		v := &taskView{NewcomerTask: t, Progress: "0"}
		done := false
		switch t.Type {
		case "bind":
			done = p.EmailVerified && p.PhoneVerified
			if done {
				v.Progress = "1"
			}
		case "first_deposit":
			done = depositDone
			if done {
				v.Progress = "1"
			}
		case "volume":
			v.Progress = vol.String()
			done = vol.GreaterThanOrEqual(t.Target)
		}
		switch id, ok := claims[t.Key]; {
		case ok:
			v.Status, v.CouponID = "claimed", &id
		case !isEnrolled || !kycOK:
			v.Status = "locked"
		case done:
			v.Status = "claimable"
		case expired:
			v.Status = "expired"
		default:
			v.Status = "in_progress"
		}
		tasks = append(tasks, v)
	}
	out["tasks"] = tasks
	return out, tasks, nil
}

func (s *Server) handleNewcomer(w http.ResponseWriter, r *http.Request, uid string) {
	out, _, err := s.newcomerState(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handleNewcomerEnroll(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	prof, err := s.profiles(ctx, []string{uid})
	if err != nil {
		writeErr(w, err)
		return
	}
	if prof[uid].KYCLevel < s.cfg.Newcomer.KYCLevel {
		writeErr(w, &userErr{Status: http.StatusForbidden, Code: "KYC_REQUIRED", Msg: "请先完成身份认证"})
		return
	}
	if err := s.ensureUser(ctx, uid); err != nil {
		writeErr(w, err)
		return
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO g_newcomer (uid, deadline) VALUES ($1, now() + make_interval(days => $2)) ON CONFLICT (uid) DO NOTHING`, uid, s.cfg.Newcomer.Days)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, conflict("ALREADY_ENROLLED", "每人限参加一次，已报名"))
		return
	}
	s.handleNewcomer(w, r, uid)
}

func (s *Server) handleNewcomerClaim(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	key := r.PathValue("key")
	out, tasks, err := s.newcomerState(ctx, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	var t *taskView
	for _, x := range tasks {
		if x.Key == key {
			t = x
		}
	}
	switch {
	case t == nil:
		writeErr(w, notFound("TASK_NOT_FOUND", "任务不存在"))
		return
	case out["enrolled"] != true:
		writeErr(w, conflict("NOT_ENROLLED", "请先报名"))
		return
	case out["kycOk"] != true:
		writeErr(w, &userErr{Status: http.StatusForbidden, Code: "KYC_REQUIRED", Msg: "请先完成身份认证"})
		return
	case t.Status == "claimed":
		writeErr(w, conflict("ALREADY_CLAIMED", "奖励已领取"))
		return
	case t.Status != "claimable":
		writeErr(w, conflict("TASK_NOT_COMPLETED", "任务未完成"))
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	title := couponTitle(t.Kind, t.Amount, s.cfg.PayoutAsset)
	cid, err := s.issueCoupon(ctx, tx, uid, t.Kind, t.Amount, title, "newcomer:"+t.Key+":"+uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO g_newcomer_claims (uid, task, coupon_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, uid, t.Key, cid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, conflict("ALREADY_CLAIMED", "奖励已领取"))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"task": t.Key, "couponId": cid, "title": title, "kind": t.Kind, "amount": t.Amount.String()})
}

// inviteKYCRewards: when an invitee reaches the KYC level within the window
// after registering, inviter and invitee each get a trial voucher.
func (s *Server) inviteKYCRewards(ctx context.Context) error {
	if s.cfg.InviteKYCReward.Sign() <= 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT uid, inviter FROM g_users WHERE inviter IS NOT NULL AND invite_reward_at IS NULL
		AND registered_at > $1 ORDER BY registered_at DESC LIMIT 200`, s.now().Add(-s.cfg.InviteKYCWithin))
	if err != nil {
		return err
	}
	pairs := map[string]string{}
	var uids []string
	for rows.Next() {
		var u, inv string
		if rows.Scan(&u, &inv) == nil {
			pairs[u] = inv
			uids = append(uids, u)
		}
	}
	rows.Close()
	prof, err := s.profiles(ctx, uids)
	if err != nil {
		return err
	}
	for u, inv := range pairs {
		if prof[u].KYCLevel < s.cfg.InviteKYCLevel {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		title := couponTitle("trial", s.cfg.InviteKYCReward, s.cfg.PayoutAsset) + "（邀请奖励）"
		_, e1 := s.issueCoupon(ctx, tx, inv, "trial", s.cfg.InviteKYCReward, title, "invite-kyc:"+u+":inviter")
		_, e2 := s.issueCoupon(ctx, tx, u, "trial", s.cfg.InviteKYCReward, title, "invite-kyc:"+u+":invitee")
		_, e3 := tx.ExecContext(ctx, `UPDATE g_users SET invite_reward_at=now() WHERE uid=$1`, u)
		if err := errors.Join(e1, e2, e3); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// handleActivities is the public activity center list.
func (s *Server) handleActivities(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	items := []map[string]any{
		{"key": "newcomer", "title": "新人限时任务", "desc": "完成任务最高领取 " + s.cfg.Newcomer.total().String() + " " + s.cfg.PayoutAsset + " 奖励",
			"status": "active", "longTerm": true, "path": "/rewards"},
		{"key": "invite", "title": "邀请好友", "desc": "好友完成身份认证，双方各得 " + s.cfg.InviteKYCReward.String() + " " + s.cfg.PayoutAsset + " 合约体验金",
			"status": "active", "longTerm": true, "path": "/invite"},
	}
	all, err := s.listCampaigns(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	for _, c := range all {
		items = append(items, map[string]any{"key": c.Key, "title": c.Title, "desc": "积分兑换体验金 / 抵扣金", "status": c.Status,
			"startsAt": c.StartsAt, "endsAt": c.EndsAt, "path": "/points"})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
