package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// LevelSync is an outstanding approved-level sync to the account service.
type LevelSync struct {
	UserID        string    `json:"userId"`
	Level         int       `json:"level"`
	Attempts      int       `json:"attempts"`
	NextAttemptAt time.Time `json:"nextAttemptAt"`
	LastError     *string   `json:"lastError,omitempty"`
}

// syncBackoff returns the delay before retry number attempts (1-based):
// base * 2^(attempts-1), capped at max.
func syncBackoff(attempts int, base, max time.Duration) time.Duration {
	if base <= 0 {
		base = 5 * time.Second
	}
	d := base
	for i := 1; i < attempts; i++ {
		d *= 2
		if max > 0 && d >= max {
			return max
		}
	}
	if max > 0 && d > max {
		return max
	}
	return d
}

// EnqueueLevelSync records that user's account level must be raised to level.
// An existing row keeps the higher level and becomes due immediately.
func (s *Store) EnqueueLevelSync(ctx context.Context, userID string, level int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO kyc_level_sync (user_id, level) VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE
		   SET level = GREATEST(kyc_level_sync.level, EXCLUDED.level),
		       next_attempt_at = now(), updated_at = now()`, userID, level)
	return err
}

// DueLevelSyncs lists syncs whose next attempt is due.
func (s *Store) DueLevelSyncs(ctx context.Context, limit int) ([]LevelSync, error) {
	return s.listLevelSyncs(ctx, `WHERE next_attempt_at <= now() ORDER BY next_attempt_at LIMIT $1`, limit)
}

// ListLevelSyncs lists all outstanding syncs (admin visibility).
func (s *Store) ListLevelSyncs(ctx context.Context, limit int) ([]LevelSync, error) {
	return s.listLevelSyncs(ctx, `ORDER BY next_attempt_at LIMIT $1`, limit)
}

func (s *Store) listLevelSyncs(ctx context.Context, tail string, limit int) ([]LevelSync, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, level, attempts, next_attempt_at, last_error FROM kyc_level_sync `+tail, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LevelSync{}
	for rows.Next() {
		var l LevelSync
		if err := rows.Scan(&l.UserID, &l.Level, &l.Attempts, &l.NextAttemptAt, &l.LastError); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CompleteLevelSync removes the row if it still targets level (a concurrent
// higher-level enqueue is kept).
func (s *Store) CompleteLevelSync(ctx context.Context, userID string, level int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM kyc_level_sync WHERE user_id = $1 AND level <= $2`, userID, level)
	return err
}

// FailLevelSync bumps attempts and schedules the next try after delay.
func (s *Store) FailLevelSync(ctx context.Context, userID string, attempts int, delay time.Duration, msg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE kyc_level_sync SET attempts = $2, next_attempt_at = now() + make_interval(secs => $3),
		        last_error = $4, updated_at = now() WHERE user_id = $1`,
		userID, attempts, delay.Seconds(), msg)
	return err
}

// trySync attempts one sync; prevAttempts is the row's attempt count so far.
func (s *Server) trySync(ctx context.Context, userID string, level, prevAttempts int) bool {
	err := s.account.SetKYCLevel(ctx, userID, level)
	if err == nil {
		if e := s.store.CompleteLevelSync(ctx, userID, level); e != nil {
			log.Printf("kyc-level sync %s: complete: %v", userID, e)
		}
		return true
	}
	n := prevAttempts + 1
	delay := syncBackoff(n, s.cfg.SyncBaseDelay, s.cfg.SyncMaxDelay)
	log.Printf("kyc-level sync %s -> L%d failed (attempt %d, retry in %s): %v", userID, level, n, delay, err)
	if e := s.store.FailLevelSync(ctx, userID, n, delay, err.Error()); e != nil {
		log.Printf("kyc-level sync %s: record failure: %v", userID, e)
	}
	return false
}

// SyncDue runs one pass over due syncs and returns how many succeeded.
func (s *Server) SyncDue(ctx context.Context) int {
	due, err := s.store.DueLevelSyncs(ctx, 100)
	if err != nil {
		log.Printf("kyc-level sync: list due: %v", err)
		return 0
	}
	ok := 0
	for _, d := range due {
		if s.trySync(ctx, d.UserID, d.Level, d.Attempts) {
			ok++
		}
	}
	return ok
}

// RunLevelSync is the background retry worker.
func (s *Server) RunLevelSync(ctx context.Context) {
	poll := s.cfg.SyncPoll
	if poll <= 0 {
		poll = 5 * time.Second
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.SyncDue(ctx)
		}
	}
}

// handleAdminLevelSync lists outstanding level syncs.
//
//	GET /admin/kyc/level-sync
func (s *Server) handleAdminLevelSync(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListLevelSyncs(r.Context(), 200)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}
