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

// Task center. Each task has a condition (first_trade/deposit/invite/volume/
// checkin/kyc) and a reward (points/coupon/asset). Progress is evaluated on
// the fly from g_fee_events, g_wallet_ops, g_users, g_checkins; claiming
// locks the reward into g_user_tasks.

type task struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Category      string          `json:"category"`
	ConditionType string          `json:"conditionType"`
	ConditionVal  decimal.Decimal `json:"conditionValue"`
	RewardKind    string          `json:"rewardKind"`
	RewardAmount  decimal.Decimal `json:"rewardAmount"`
	RewardAsset   string          `json:"rewardAsset"`
	Repeatable    bool            `json:"repeatable"`
	Daily         bool            `json:"daily"`
	Sort          int             `json:"sort"`
}

type taskWithProgress struct {
	task
	Progress  string `json:"progress"`
	Target    string `json:"target"`
	Status    string `json:"status"` // in_progress | claimable | claimed | expired
	ClaimedAt *int64 `json:"claimedAt,omitempty"`
	RewardRef string `json:"rewardRef,omitempty"`
}

func (s *Server) taskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /campaigns/tasks", s.user(s.handleListTasks))
	mux.HandleFunc("POST /campaigns/tasks/{id}/claim", s.user(s.handleClaimTask))
	mux.HandleFunc("POST /campaigns/tasks/checkin", s.user(s.handleCheckin))
	mux.HandleFunc("GET /campaigns/checkins", s.user(s.handleCheckinHistory))
}

func (s *Server) loadTasks(ctx context.Context) ([]task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, description, category, condition_type, condition_value,
		reward_kind, reward_amount, reward_asset, repeatable, daily, sort
		FROM g_tasks WHERE enabled ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []task{}
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Category, &t.ConditionType, &t.ConditionVal,
			&t.RewardKind, &t.RewardAmount, &t.RewardAsset, &t.Repeatable, &t.Daily, &t.Sort); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Server) taskProgress(ctx context.Context, uid string, t task, day time.Time) (progress decimal.Decimal, done bool, err error) {
	switch t.ConditionType {
	case "deposit":
		quotes := usdQuotesSlice(s.cfg)
		if t.ConditionVal.IsZero() {
			var n int
			err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM g_wallet_ops WHERE uid=$1 AND op='deposit' AND asset = ANY($2)`, uid, quotes).Scan(&n)
			if err == nil && n > 0 {
				return decimal.NewFromInt(1), true, nil
			}
			return decimal.Zero, false, err
		}
		err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_wallet_ops WHERE uid=$1 AND op='deposit' AND asset = ANY($2)`, uid, quotes).Scan(&progress)
		return progress, progress.GreaterThanOrEqual(t.ConditionVal), err
	case "first_trade":
		var n int
		err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM g_fee_events WHERE uid=$1 LIMIT 1`, uid).Scan(&n)
		if err == nil && n > 0 {
			return decimal.NewFromInt(1), true, nil
		}
		return decimal.Zero, false, err
	case "kyc":
		var kyc int
		err = s.db.QueryRowContext(ctx, `SELECT kyc_level FROM g_users WHERE uid=$1`, uid).Scan(&kyc)
		if err == nil {
			ok := decimal.NewFromInt(int64(kyc)).GreaterThanOrEqual(t.ConditionVal)
			return decimal.NewFromInt(int64(kyc)), ok, nil
		}
		return decimal.Zero, false, err
	case "invite":
		var n int
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM g_users WHERE inviter=$1 AND kyc_level >= 1`, uid).Scan(&n)
		if err == nil {
			p := decimal.NewFromInt(int64(n))
			return p, p.GreaterThanOrEqual(t.ConditionVal), nil
		}
		return decimal.Zero, false, err
	case "volume":
		q := `SELECT COALESCE(SUM(notional),0) FROM g_fee_events WHERE uid=$1`
		args := []any{uid}
		if t.Daily {
			start := utcDay(day)
			q += ` AND at >= $2 AND at < $3`
			args = append(args, start, start.Add(24*time.Hour))
		}
		err = s.db.QueryRowContext(ctx, q, args...).Scan(&progress)
		return progress, progress.GreaterThanOrEqual(t.ConditionVal), err
	case "checkin":
		start := utcDay(day)
		var n int
		err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM g_checkins WHERE uid=$1 AND day=$2`, uid, start).Scan(&n)
		if err == nil && n > 0 {
			return decimal.NewFromInt(1), true, nil
		}
		return decimal.Zero, false, err
	}
	return decimal.Zero, false, nil
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	if err := s.ensureUser(ctx, uid); err != nil {
		writeErr(w, err)
		return
	}
	tasks, err := s.loadTasks(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	day := utcDay(s.now())
	// Preload claim rows: for daily tasks claim is per-day; for one-shot the
	// sentinel day is epoch.
	claims, err := s.loadTaskClaims(ctx, uid, day)
	if err != nil {
		writeErr(w, err)
		return
	}
	items := []taskWithProgress{}
	for _, t := range tasks {
		v := taskWithProgress{task: t, Target: t.ConditionVal.String()}
		key := t.ID + "/"
		if t.Daily {
			key += day.Format("2006-01-02")
		} else {
			key += "1970-01-01"
		}
		if c, ok := claims[key]; ok {
			v.Status = c.status
			if c.status == "claimed" {
				v.Progress = t.ConditionVal.String()
				ms := c.claimedAt.UnixMilli()
				v.ClaimedAt = &ms
				v.RewardRef = c.rewardRef
				items = append(items, v)
				continue
			}
		}
		prog, done, err := s.taskProgress(ctx, uid, t, s.now())
		if err != nil {
			writeErr(w, err)
			return
		}
		v.Progress = prog.String()
		switch {
		case done:
			v.Status = "claimable"
		default:
			v.Status = "in_progress"
		}
		items = append(items, v)
	}
	// Balance so the UI can show it alongside tasks.
	bal, _ := s.pointsBalance2(ctx, uid)
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "pointsBalance": bal})
}

type taskClaim struct {
	status    string
	claimedAt time.Time
	rewardRef string
}

func (s *Server) loadTaskClaims(ctx context.Context, uid string, day time.Time) (map[string]taskClaim, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT task_id, day, status, COALESCE(claimed_at, '1970-01-01'::timestamptz), reward_ref
		FROM g_user_tasks WHERE uid=$1 AND (day=$2 OR day='1970-01-01'::date)`, uid, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]taskClaim{}
	for rows.Next() {
		var id, status, ref string
		var d time.Time
		var at time.Time
		if err := rows.Scan(&id, &d, &status, &at, &ref); err != nil {
			return nil, err
		}
		out[id+"/"+d.Format("2006-01-02")] = taskClaim{status: status, claimedAt: at, rewardRef: ref}
	}
	return out, rows.Err()
}

func (s *Server) handleClaimTask(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	id := r.PathValue("id")
	tasks, err := s.loadTasks(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	var t *task
	for i := range tasks {
		if tasks[i].ID == id {
			t = &tasks[i]
			break
		}
	}
	if t == nil {
		writeErr(w, notFound("TASK_NOT_FOUND", "任务不存在"))
		return
	}
	if err := s.ensureUser(ctx, uid); err != nil {
		writeErr(w, err)
		return
	}
	prog, done, err := s.taskProgress(ctx, uid, *t, s.now())
	if err != nil {
		writeErr(w, err)
		return
	}
	if !done {
		writeErr(w, conflict("TASK_NOT_COMPLETED", "任务未完成"))
		return
	}
	day := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	if t.Daily {
		day = utcDay(s.now())
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	// Insert claim row; if already claimed, fail.
	res, err := tx.ExecContext(ctx, `INSERT INTO g_user_tasks (uid, task_id, day, progress, status, claimed_at)
		VALUES ($1,$2,$3,$4,'claimed', now()) ON CONFLICT (uid, task_id, day) DO UPDATE
		SET status='claimed', claimed_at=now(), progress=EXCLUDED.progress
		WHERE g_user_tasks.status <> 'claimed'`, uid, t.ID, day, prog)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, conflict("ALREADY_CLAIMED", "奖励已领取"))
		return
	}
	ref, err := s.applyReward(ctx, tx, uid, *t, day)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE g_user_tasks SET reward_ref=$4 WHERE uid=$1 AND task_id=$2 AND day=$3`, uid, t.ID, day, ref); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	bal, _ := s.pointsBalance2(ctx, uid)
	common.WriteJSON(w, http.StatusOK, map[string]any{"task": t.ID, "rewardKind": t.RewardKind,
		"rewardAmount": t.RewardAmount.String(), "rewardAsset": t.RewardAsset, "ref": ref, "pointsBalance": bal})
}

// applyReward writes the reward side-effects (coupon, points credit, asset
// transfer). Returns an idempotency reference.
func (s *Server) applyReward(ctx context.Context, tx *sql.Tx, uid string, t task, day time.Time) (string, error) {
	ref := "task:" + t.ID + ":" + uid + ":" + day.Format("2006-01-02")
	switch t.RewardKind {
	case "points":
		if err := s.creditPoints(ctx, tx, uid, t.RewardAmount.IntPart(), "task:"+t.ID, ref); err != nil {
			return "", err
		}
	case "coupon_trial":
		title := couponTitle("trial", t.RewardAmount, t.RewardAsset)
		if _, err := s.issueCoupon(ctx, tx, uid, "trial", t.RewardAmount, title, ref); err != nil {
			return "", err
		}
	case "coupon_deduction":
		title := couponTitle("deduction", t.RewardAmount, t.RewardAsset)
		if _, err := s.issueCoupon(ctx, tx, uid, "deduction", t.RewardAmount, title, ref); err != nil {
			return "", err
		}
	case "asset":
		// Credit spot via asset system transfer (idempotent on ref).
		if err := s.systemTransfer(ctx, uid, t.RewardAsset, "spot", "credit", t.RewardAmount, ref); err != nil {
			return "", err
		}
	default:
		return "", errors.New("unknown reward kind: " + t.RewardKind)
	}
	return ref, nil
}

// --- daily check-in ----------------------------------------------------

func (s *Server) handleCheckin(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	if err := s.ensureUser(ctx, uid); err != nil {
		writeErr(w, err)
		return
	}
	day := utcDay(s.now())
	var prevStreak int
	_ = s.db.QueryRowContext(ctx, `SELECT streak FROM g_checkins WHERE uid=$1 AND day=$2`, uid, day.Add(-24*time.Hour)).Scan(&prevStreak)
	res, err := s.db.ExecContext(ctx, `INSERT INTO g_checkins (uid, day, streak) VALUES ($1,$2,$3)
		ON CONFLICT (uid, day) DO NOTHING`, uid, day, prevStreak+1)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, conflict("ALREADY_CHECKED_IN", "今日已签到"))
		return
	}
	var streak int
	_ = s.db.QueryRowContext(ctx, `SELECT streak FROM g_checkins WHERE uid=$1 AND day=$2`, uid, day).Scan(&streak)
	common.WriteJSON(w, http.StatusOK, map[string]any{"day": day.Format("2006-01-02"), "streak": streak,
		"message": "签到成功，可前往任务中心领取今日签到积分"})
}

func (s *Server) handleCheckinHistory(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT day, streak FROM g_checkins WHERE uid=$1 ORDER BY day DESC LIMIT 60`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var d time.Time
		var st int
		if err := rows.Scan(&d, &st); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{"day": d.Format("2006-01-02"), "streak": st})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func usdQuotesSlice(cfg Config) []string {
	out := []string{}
	for q := range cfg.USDQuotes {
		out = append(out, q)
	}
	return out
}

var _ = strings.TrimSpace
