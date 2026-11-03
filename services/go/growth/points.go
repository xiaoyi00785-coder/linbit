package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Points carnival (积分嘉年华). Users enroll in a period; daily tasks (reset
// 00:00 UTC) earn points only after enrollment and inside the period; points
// are redeemed for vouchers, each tier once per user per period. Points are
// scoped to the period, so they lapse when it ends.

type VolumeTier struct {
	Volume decimal.Decimal `json:"volume"`
	Points int             `json:"points"`
}

type RedeemTier struct {
	Points int             `json:"points"`
	Kind   string          `json:"kind"` // trial | deduction
	Amount decimal.Decimal `json:"amount"`
}

type PointsRules struct {
	VolumeTiers  []VolumeTier    `json:"volumeTiers"`
	DepositUnit  decimal.Decimal `json:"depositUnit"`
	DepositCap   int             `json:"depositCap"`
	InvitePoints int             `json:"invitePoints"`
	InviteCap    int             `json:"inviteCap"`
	Redeem       []RedeemTier    `json:"redeem"`
}

const (
	defaultVolumeTiers = "2000:1,10000:1,100000:3,200000:3,300000:5,500000:8,1000000:12,3000000:35,10000000:100"
	defaultRedeem      = "10:deduction:3,20:deduction:6,35:trial:10,50:deduction:10,75:trial:20,100:trial:30,200:deduction:75,300:trial:100"
)

func loadPointsRules() PointsRules {
	p := PointsRules{
		DepositUnit:  dec("GROWTH_POINTS_DEPOSIT_UNIT", "100"),
		DepositCap:   common.EnvInt("GROWTH_POINTS_DEPOSIT_DAILY_CAP", 10),
		InvitePoints: common.EnvInt("GROWTH_POINTS_INVITE_POINTS", 8),
		InviteCap:    common.EnvInt("GROWTH_POINTS_INVITE_DAILY_CAP", 80),
	}
	p.VolumeTiers = parseVolumeTiers(common.Env("GROWTH_POINTS_VOLUME_TIERS", defaultVolumeTiers))
	if len(p.VolumeTiers) == 0 {
		p.VolumeTiers = parseVolumeTiers(defaultVolumeTiers)
	}
	p.Redeem = parseRedeem(common.Env("GROWTH_POINTS_REDEEM", defaultRedeem))
	if len(p.Redeem) == 0 {
		p.Redeem = parseRedeem(defaultRedeem)
	}
	return p
}

func parseVolumeTiers(s string) []VolumeTier {
	var out []VolumeTier
	for _, part := range strings.Split(s, ",") {
		f := strings.Split(strings.TrimSpace(part), ":")
		if len(f) != 2 {
			continue
		}
		v, e1 := decimal.NewFromString(f[0])
		n, e2 := strconv.Atoi(f[1])
		if e1 == nil && e2 == nil && n > 0 {
			out = append(out, VolumeTier{v, n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Volume.LessThan(out[j].Volume) })
	return out
}

func parseRedeem(s string) []RedeemTier {
	var out []RedeemTier
	for _, part := range strings.Split(s, ",") {
		f := strings.Split(strings.TrimSpace(part), ":")
		if len(f) != 3 || (f[1] != "trial" && f[1] != "deduction") {
			continue
		}
		n, e1 := strconv.Atoi(f[0])
		a, e2 := decimal.NewFromString(f[2])
		if e1 == nil && e2 == nil && n > 0 && a.Sign() > 0 {
			out = append(out, RedeemTier{n, f[1], a})
		}
	}
	return out
}

// volumePoints: cumulative — every threshold reached in the day adds its points.
func (p PointsRules) volumePoints(vol decimal.Decimal) int {
	n := 0
	for _, t := range p.VolumeTiers {
		if vol.GreaterThanOrEqual(t.Volume) {
			n += t.Points
		}
	}
	return n
}

func (p PointsRules) depositPoints(amt decimal.Decimal) int {
	if p.DepositUnit.Sign() <= 0 {
		return 0
	}
	n := int(amt.Div(p.DepositUnit).IntPart())
	if n > p.DepositCap {
		n = p.DepositCap
	}
	return n
}

func (p PointsRules) invitePoints(count int) int {
	n := count * p.InvitePoints
	if n > p.InviteCap {
		n = p.InviteCap
	}
	return n
}

func (p PointsRules) dailyMax() int {
	return p.volumePoints(decimal.New(1, 30)) + p.DepositCap + p.InviteCap
}

type Campaign struct {
	ID       int64       `json:"id"`
	Key      string      `json:"key"`
	Title    string      `json:"title"`
	StartsAt int64       `json:"startsAt"`
	EndsAt   int64       `json:"endsAt"`
	Status   string      `json:"status"` // upcoming | active | ended
	Rules    PointsRules `json:"rules"`
	DailyMax int         `json:"dailyMaxVolumePoints"`
	starts   time.Time
	ends     time.Time
}

func (s *Server) campaignStatus(c *Campaign) string {
	now := s.now()
	switch {
	case now.Before(c.starts):
		return "upcoming"
	case now.Before(c.ends):
		return "active"
	}
	return "ended"
}

const campaignCols = `id, key, title, starts_at, ends_at, config`

func (s *Server) scanCampaign(sc interface{ Scan(...any) error }) (*Campaign, error) {
	c := &Campaign{}
	var cfg []byte
	if err := sc.Scan(&c.ID, &c.Key, &c.Title, &c.starts, &c.ends, &cfg); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(cfg, &c.Rules); err != nil {
		return nil, err
	}
	c.StartsAt, c.EndsAt = c.starts.UnixMilli(), c.ends.UnixMilli()
	c.Status = s.campaignStatus(c)
	c.DailyMax = c.Rules.volumePoints(decimal.New(1, 30))
	return c, nil
}

func (s *Server) listCampaigns(ctx context.Context) ([]*Campaign, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+campaignCols+` FROM g_campaigns WHERE kind='points' ORDER BY starts_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Campaign{}
	for rows.Next() {
		c, err := s.scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// currentCampaign: ?campaign=key, else the active one, else the next upcoming,
// else the most recent.
func (s *Server) currentCampaign(ctx context.Context, key string) (*Campaign, error) {
	all, err := s.listCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	if key != "" {
		for _, c := range all {
			if c.Key == key {
				return c, nil
			}
		}
		return nil, notFound("CAMPAIGN_NOT_FOUND", "活动不存在")
	}
	for _, want := range []string{"active", "upcoming", "ended"} {
		var pick *Campaign
		for _, c := range all { // all is newest first
			if c.Status == want {
				if want == "upcoming" {
					pick = c // keep going: the earliest upcoming
				} else if pick == nil {
					pick = c
				}
			}
		}
		if pick != nil {
			return pick, nil
		}
	}
	return nil, notFound("CAMPAIGN_NOT_FOUND", "暂无积分活动")
}

func (s *Server) pointsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /points/campaigns", func(w http.ResponseWriter, r *http.Request) {
		all, err := s.listCampaigns(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": all})
	})
	mux.HandleFunc("GET /points/campaign", func(w http.ResponseWriter, r *http.Request) {
		c, err := s.currentCampaign(r.Context(), r.URL.Query().Get("campaign"))
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, c)
	})
	mux.HandleFunc("POST /points/enroll", s.user(s.handleEnroll))
	mux.HandleFunc("GET /points/me", s.user(s.handlePointsMe))
	mux.HandleFunc("GET /points/history", s.user(s.handlePointsHistory))
	mux.HandleFunc("POST /points/redeem", s.user(s.handleRedeem))
	mux.HandleFunc("GET /points/redemptions", s.user(s.handleRedemptions))
	mux.HandleFunc("GET /internal/points/campaigns", s.internal(func(w http.ResponseWriter, r *http.Request) {
		all, err := s.listCampaigns(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": all})
	}))
	mux.HandleFunc("POST /internal/points/campaigns", s.internal(s.handleCreateCampaign))
}

type campaignBody struct {
	Key      string       `json:"key"`
	Title    string       `json:"title"`
	StartsAt int64        `json:"startsAt"`
	EndsAt   int64        `json:"endsAt"`
	Rules    *PointsRules `json:"rules"`
	Operator string       `json:"operator"`
}

// handleCreateCampaign schedules a new period (admin, reward:edit). Rules
// default to the GROWTH_POINTS_* configuration. Periods may not overlap.
func (s *Server) handleCreateCampaign(w http.ResponseWriter, r *http.Request) {
	var b campaignBody
	if err := common.DecodeJSON(r, &b); err != nil {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
		return
	}
	key, ok1 := clean(b.Key, 40)
	title, ok2 := clean(b.Title, 64)
	if key == "" || title == "" || !ok1 || !ok2 || b.EndsAt <= b.StartsAt {
		writeErr(w, bad("INVALID_CAMPAIGN", "请填写活动标识、标题与有效的起止时间"))
		return
	}
	rules := s.cfg.Points
	if b.Rules != nil {
		rules = *b.Rules
	}
	if len(rules.VolumeTiers) == 0 || len(rules.Redeem) == 0 {
		writeErr(w, bad("INVALID_CAMPAIGN", "规则不完整"))
		return
	}
	seen := map[int]bool{}
	for _, t := range rules.Redeem {
		if t.Points <= 0 || t.Amount.Sign() <= 0 || (t.Kind != "trial" && t.Kind != "deduction") || seen[t.Points] {
			writeErr(w, bad("INVALID_CAMPAIGN", "兑换档位无效"))
			return
		}
		seen[t.Points] = true
	}
	cfg, _ := json.Marshal(rules)
	st, en := time.UnixMilli(b.StartsAt), time.UnixMilli(b.EndsAt)
	var overlap int
	_ = s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM g_campaigns WHERE kind='points' AND starts_at < $2 AND ends_at > $1`, st, en).Scan(&overlap)
	if overlap > 0 {
		writeErr(w, conflict("CAMPAIGN_OVERLAP", "与已有活动时间重叠"))
		return
	}
	row := s.db.QueryRowContext(r.Context(), `INSERT INTO g_campaigns (key, title, starts_at, ends_at, config, created_by) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (key) DO NOTHING RETURNING `+campaignCols, key, title, st, en, cfg, b.Operator)
	c, err := s.scanCampaign(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, conflict("CAMPAIGN_EXISTS", "活动标识已存在"))
		return
	} else if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, c)
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	var b struct {
		Campaign string `json:"campaign"`
	}
	_ = common.DecodeJSON(r, &b)
	c, err := s.currentCampaign(ctx, b.Campaign)
	if err != nil {
		writeErr(w, err)
		return
	}
	switch c.Status {
	case "ended":
		writeErr(w, conflict("CAMPAIGN_ENDED", "活动已结束"))
		return
	case "upcoming":
		writeErr(w, conflict("CAMPAIGN_NOT_STARTED", "活动尚未开始"))
		return
	}
	if err := s.ensureUser(ctx, uid); err != nil {
		writeErr(w, err)
		return
	}
	var at time.Time
	if err := s.db.QueryRowContext(ctx, `INSERT INTO g_enrollments (campaign_id, uid) VALUES ($1,$2)
		ON CONFLICT (campaign_id, uid) DO UPDATE SET at=g_enrollments.at RETURNING at`, c.ID, uid).Scan(&at); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"campaign": c.Key, "enrolledAt": at.UnixMilli()})
}

// balance returns earned, redeemed points of uid in a campaign.
func (s *Server) pointsBalance(ctx context.Context, q rowQueryer, cid int64, uid string) (earned, spent int, err error) {
	if err = q.QueryRowContext(ctx, `SELECT COALESCE(SUM(points),0) FROM g_points_daily WHERE campaign_id=$1 AND uid=$2`, cid, uid).Scan(&earned); err != nil {
		return
	}
	err = q.QueryRowContext(ctx, `SELECT COALESCE(SUM(points),0) FROM g_redemptions WHERE campaign_id=$1 AND uid=$2`, cid, uid).Scan(&spent)
	return
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Server) handlePointsMe(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	c, err := s.currentCampaign(ctx, r.URL.Query().Get("campaign"))
	if err != nil {
		writeErr(w, err)
		return
	}
	out := map[string]any{"campaign": c.Key, "status": c.Status, "enrolled": false, "balance": 0, "earned": 0, "redeemed": 0,
		"redeemedTiers": []int{}}
	var at time.Time
	err = s.db.QueryRowContext(ctx, `SELECT at FROM g_enrollments WHERE campaign_id=$1 AND uid=$2`, c.ID, uid).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteJSON(w, http.StatusOK, out)
		return
	} else if err != nil {
		writeErr(w, err)
		return
	}
	earned, spent, err := s.pointsBalance(ctx, s.db, c.ID, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	bal := earned - spent
	if c.Status == "ended" {
		bal = 0 // points lapse at the end of the period
	}
	out["enrolled"], out["enrolledAt"], out["earned"], out["redeemed"], out["balance"] = true, at.UnixMilli(), earned, spent, bal
	tiers := []int{}
	rows, err := s.db.QueryContext(ctx, `SELECT points FROM g_redemptions WHERE campaign_id=$1 AND uid=$2 ORDER BY points`, c.ID, uid)
	if err == nil {
		for rows.Next() {
			var p int
			if rows.Scan(&p) == nil {
				tiers = append(tiers, p)
			}
		}
		rows.Close()
	}
	out["redeemedTiers"] = tiers
	// Today's task progress (UTC day).
	today := map[string]any{}
	for _, t := range []string{"volume", "deposit", "invite"} {
		today[t] = map[string]any{"progress": "0", "points": 0}
	}
	rows, err = s.db.QueryContext(ctx, `SELECT task, progress, points FROM g_points_daily WHERE campaign_id=$1 AND uid=$2 AND day=$3`, c.ID, uid, utcDay(s.now()))
	if err == nil {
		for rows.Next() {
			var task string
			var prog decimal.Decimal
			var p int
			if rows.Scan(&task, &prog, &p) == nil {
				today[task] = map[string]any{"progress": prog.String(), "points": p}
			}
		}
		rows.Close()
	}
	out["today"] = today
	out["resetAt"] = utcDay(s.now()).Add(24 * time.Hour).UnixMilli()
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handlePointsHistory(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	c, err := s.currentCampaign(ctx, r.URL.Query().Get("campaign"))
	if err != nil {
		writeErr(w, err)
		return
	}
	type item struct {
		Kind   string `json:"kind"` // earn task (volume|deposit|invite) or redeem
		Day    string `json:"day,omitempty"`
		Points int    `json:"points"`
		Detail string `json:"detail"`
		Time   int64  `json:"time"`
	}
	items := []item{}
	rows, err := s.db.QueryContext(ctx, `SELECT day, task, progress, points FROM g_points_daily WHERE campaign_id=$1 AND uid=$2 AND points > 0`, c.ID, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	for rows.Next() {
		var d time.Time
		var task string
		var prog decimal.Decimal
		var p int
		if err := rows.Scan(&d, &task, &prog, &p); err == nil {
			items = append(items, item{Kind: task, Day: d.Format("2006-01-02"), Points: p, Detail: prog.String(), Time: d.Add(24*time.Hour - time.Millisecond).UnixMilli()})
		}
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT r.points, c.title, r.at FROM g_redemptions r JOIN g_coupons c ON c.id=r.coupon_id WHERE r.campaign_id=$1 AND r.uid=$2`, c.ID, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	for rows.Next() {
		var p int
		var title string
		var at time.Time
		if err := rows.Scan(&p, &title, &at); err == nil {
			items = append(items, item{Kind: "redeem", Points: -p, Detail: title, Time: at.UnixMilli()})
		}
	}
	rows.Close()
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time > items[j].Time })
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func couponTitle(kind string, amt decimal.Decimal, asset string) string {
	if kind == "trial" {
		return amt.String() + " " + asset + " 体验金"
	}
	return amt.String() + " " + asset + " 抵扣金"
}

func (s *Server) handleRedeem(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	var b struct {
		Campaign string `json:"campaign"`
		Points   int    `json:"points"`
	}
	if err := common.DecodeJSON(r, &b); err != nil {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
		return
	}
	c, err := s.currentCampaign(ctx, b.Campaign)
	if err != nil {
		writeErr(w, err)
		return
	}
	if c.Status != "active" {
		writeErr(w, conflict("CAMPAIGN_ENDED", "活动未在进行中，无法兑换"))
		return
	}
	var tier *RedeemTier
	for i := range c.Rules.Redeem {
		if c.Rules.Redeem[i].Points == b.Points {
			tier = &c.Rules.Redeem[i]
		}
	}
	if tier == nil {
		writeErr(w, bad("INVALID_TIER", "兑换档位不存在"))
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	// Lock the enrollment row: serializes redemptions per user and campaign.
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM g_enrollments WHERE campaign_id=$1 AND uid=$2 FOR UPDATE`, c.ID, uid).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		writeErr(w, conflict("NOT_ENROLLED", "请先报名参加活动"))
		return
	} else if err != nil {
		writeErr(w, err)
		return
	}
	var dup int
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM g_redemptions WHERE campaign_id=$1 AND uid=$2 AND points=$3`, c.ID, uid, tier.Points).Scan(&dup)
	if dup > 0 {
		writeErr(w, conflict("ALREADY_REDEEMED", "该档位已兑换（每档限兑一次）"))
		return
	}
	earned, spent, err := s.pointsBalance(ctx, tx, c.ID, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if earned-spent < tier.Points {
		writeErr(w, conflict("INSUFFICIENT_POINTS", "积分不足"))
		return
	}
	title := couponTitle(tier.Kind, tier.Amount, s.cfg.PayoutAsset)
	cid, err := s.issueCoupon(ctx, tx, uid, tier.Kind, tier.Amount, title, "points:"+c.Key+":"+strconv.Itoa(tier.Points)+":"+uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO g_redemptions (campaign_id, uid, points, coupon_id) VALUES ($1,$2,$3,$4)`, c.ID, uid, tier.Points, cid); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"couponId": cid, "title": title, "kind": tier.Kind, "amount": tier.Amount.String(),
		"points": tier.Points, "balance": earned - spent - tier.Points})
}

func (s *Server) handleRedemptions(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	c, err := s.currentCampaign(ctx, r.URL.Query().Get("campaign"))
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.points, r.coupon_id, c.title, c.kind, c.amount, c.status, r.at FROM g_redemptions r
		JOIN g_coupons c ON c.id=r.coupon_id WHERE r.campaign_id=$1 AND r.uid=$2 ORDER BY r.at DESC`, c.ID, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var p int
		var id int64
		var title, kind, st string
		var amt decimal.Decimal
		var at time.Time
		if err := rows.Scan(&p, &id, &title, &kind, &amt, &st, &at); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{"points": p, "couponId": id, "title": title, "kind": kind, "amount": amt.String(), "couponStatus": st, "time": at.UnixMilli()})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// accruePoints recomputes the task points of enrolled users for the UTC days
// touched recently (today and yesterday) of every running period. It is run
// after each feed sync; recomputation is idempotent.
func (s *Server) accruePoints(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE g_users u SET valid_at = now() WHERE valid_at IS NULL
		AND (SELECT COALESCE(SUM(fee),0) FROM g_fee_events f WHERE f.uid=u.uid) >= $1`, s.cfg.ValidInviteeFees); err != nil {
		return err
	}
	all, err := s.listCampaigns(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	for _, c := range all {
		if now.Before(c.starts) || now.After(c.ends.Add(24*time.Hour)) {
			continue
		}
		for d := utcDay(now).Add(-24 * time.Hour); !d.After(now); d = d.Add(24 * time.Hour) {
			from, to := d, d.Add(24*time.Hour)
			if from.Before(c.starts) {
				from = c.starts
			}
			if to.After(c.ends) {
				to = c.ends
			}
			if !from.Before(to) {
				continue
			}
			if err := s.accrueDay(ctx, c, d, from, to); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) accrueDay(ctx context.Context, c *Campaign, day, from, to time.Time) error {
	quotes := []string{}
	for q := range s.cfg.USDQuotes {
		quotes = append(quotes, q)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.uid,
		  (SELECT COALESCE(SUM(notional),0) FROM g_fee_events f WHERE f.uid=e.uid AND f.product='futures' AND f.at >= GREATEST($2, e.at) AND f.at < $3),
		  (SELECT COALESCE(SUM(amount),0) FROM g_wallet_ops o WHERE o.uid=e.uid AND o.op='deposit' AND o.asset = ANY($4) AND o.at >= GREATEST($2, e.at) AND o.at < $3),
		  (SELECT count(*) FROM g_users i WHERE i.inviter=e.uid AND i.valid_at >= GREATEST($2, e.at) AND i.valid_at < $3)
		FROM g_enrollments e WHERE e.campaign_id=$1 AND e.at < $3`, c.ID, from, to, quotes)
	if err != nil {
		return err
	}
	type acc struct {
		uid       string
		vol, depo decimal.Decimal
		inv       int
	}
	var list []acc
	for rows.Next() {
		var a acc
		if err := rows.Scan(&a.uid, &a.vol, &a.depo, &a.inv); err != nil {
			rows.Close()
			return err
		}
		list = append(list, a)
	}
	rows.Close()
	for _, a := range list {
		for _, t := range []struct {
			task string
			prog decimal.Decimal
			pts  int
		}{
			{"volume", a.vol, c.Rules.volumePoints(a.vol)},
			{"deposit", a.depo, c.Rules.depositPoints(a.depo)},
			{"invite", decimal.NewFromInt(int64(a.inv)), c.Rules.invitePoints(a.inv)},
		} {
			if t.prog.IsZero() {
				continue
			}
			// Points never decrease once credited (GREATEST), e.g. if a
			// trade row were corrected later.
			if _, err := s.db.ExecContext(ctx, `INSERT INTO g_points_daily (campaign_id, uid, day, task, progress, points) VALUES ($1,$2,$3,$4,$5,$6)
				ON CONFLICT (campaign_id, uid, day, task) DO UPDATE SET progress=EXCLUDED.progress, points=GREATEST(g_points_daily.points, EXCLUDED.points)`,
				c.ID, a.uid, day, t.task, t.prog.Round(8), t.pts); err != nil {
				return err
			}
		}
	}
	return nil
}
