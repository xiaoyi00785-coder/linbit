package main

import (
	"context"
	"database/sql"
	"time"

	"github.com/shopspring/decimal"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ---------- exports ----------

type ExportJob struct {
	ID            int64
	JobID         string
	UserID        string
	Kind          string
	Format        string
	FromAt        sql.NullTime
	ToAt          sql.NullTime
	Status        string
	FilePath      sql.NullString
	SizeBytes     sql.NullInt64
	RowCount      sql.NullInt32
	DownloadToken sql.NullString
	Error         sql.NullString
	CreatedAt     time.Time
	StartedAt     sql.NullTime
	FinishedAt    sql.NullTime
	ExpiresAt     sql.NullTime
}

func (s *Store) InsertExport(ctx context.Context, j *ExportJob) error {
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO report_exports (job_id, user_id, kind, format, from_at, to_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,'queued')
		RETURNING id, created_at`,
		j.JobID, j.UserID, j.Kind, j.Format, j.FromAt, j.ToAt,
	).Scan(&j.ID, &j.CreatedAt); err != nil {
		return err
	}
	j.Status = "queued"
	return nil
}

// LeaseNextExport atomically picks the oldest queued job and marks it running.
func (s *Store) LeaseNextExport(ctx context.Context) (*ExportJob, error) {
	j := &ExportJob{}
	err := s.db.QueryRowContext(ctx, `
		UPDATE report_exports SET status='running', started_at=now()
		WHERE id = (
		  SELECT id FROM report_exports
		  WHERE status='queued'
		  ORDER BY created_at ASC
		  FOR UPDATE SKIP LOCKED
		  LIMIT 1
		)
		RETURNING id, job_id, user_id, kind, format, from_at, to_at, status, created_at, started_at`,
	).Scan(&j.ID, &j.JobID, &j.UserID, &j.Kind, &j.Format, &j.FromAt, &j.ToAt, &j.Status, &j.CreatedAt, &j.StartedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func (s *Store) CompleteExport(ctx context.Context, id int64, path, token string, rows int, size int64, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE report_exports SET status='ready', file_path=$2, download_token=$3,
		  row_count=$4, size_bytes=$5, finished_at=now(), expires_at=$6
		WHERE id=$1`, id, path, token, rows, size, expires)
	return err
}

func (s *Store) FailExport(ctx context.Context, id int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE report_exports SET status='failed', error=$2, finished_at=now()
		WHERE id=$1`, id, errMsg)
	return err
}

func (s *Store) GetExport(ctx context.Context, userID, jobID string) (*ExportJob, error) {
	j := &ExportJob{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, job_id, user_id, kind, format, from_at, to_at, status, file_path, size_bytes,
		       row_count, download_token, error, created_at, started_at, finished_at, expires_at
		FROM report_exports WHERE user_id=$1 AND job_id=$2`, userID, jobID).Scan(
		&j.ID, &j.JobID, &j.UserID, &j.Kind, &j.Format, &j.FromAt, &j.ToAt, &j.Status, &j.FilePath,
		&j.SizeBytes, &j.RowCount, &j.DownloadToken, &j.Error, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

// GetExportByJobID is the token-authed lookup used by the download handler.
// It does not scope by user because the download token itself is the credential.
func (s *Store) GetExportByJobID(ctx context.Context, jobID string) (*ExportJob, error) {
	j := &ExportJob{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, job_id, user_id, kind, format, from_at, to_at, status, file_path, size_bytes,
		       row_count, download_token, error, created_at, started_at, finished_at, expires_at
		FROM report_exports WHERE job_id=$1`, jobID).Scan(
		&j.ID, &j.JobID, &j.UserID, &j.Kind, &j.Format, &j.FromAt, &j.ToAt, &j.Status, &j.FilePath,
		&j.SizeBytes, &j.RowCount, &j.DownloadToken, &j.Error, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func (s *Store) ListExports(ctx context.Context, userID string, limit int) ([]*ExportJob, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, job_id, user_id, kind, format, from_at, to_at, status, file_path, size_bytes,
		       row_count, download_token, error, created_at, started_at, finished_at, expires_at
		FROM report_exports WHERE user_id=$1
		ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ExportJob
	for rows.Next() {
		j := &ExportJob{}
		if err := rows.Scan(&j.ID, &j.JobID, &j.UserID, &j.Kind, &j.Format, &j.FromAt, &j.ToAt, &j.Status,
			&j.FilePath, &j.SizeBytes, &j.RowCount, &j.DownloadToken, &j.Error, &j.CreatedAt, &j.StartedAt,
			&j.FinishedAt, &j.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ExpireReady marks ready exports whose files have aged past their TTL. The
// janitor deletes the files on disk; the row stays so users can see history.
func (s *Store) ExpireReady(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, file_path FROM report_exports
		WHERE status='ready' AND expires_at IS NOT NULL AND expires_at < now()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	var ids []int64
	for rows.Next() {
		var id int64
		var p sql.NullString
		if err := rows.Scan(&id, &p); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		if p.Valid {
			paths = append(paths, p.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE report_exports SET status='expired', file_path=NULL, download_token=NULL WHERE id=$1`, id); err != nil {
			return paths, err
		}
	}
	return paths, nil
}

// ---------- subscriptions ----------

type Subscription struct {
	ID        int64
	UserID    string
	Asset     string
	Chain     string
	Address   string
	MinAmount decimal.NullDecimal
	Channel   string
	Active    bool
	CreatedAt time.Time
}

func (s *Store) InsertSubscription(ctx context.Context, sub *Subscription) error {
	return s.db.QueryRowContext(ctx, `
		INSERT INTO notification_subscriptions (user_id, asset, chain, address_filter, min_amount, channel)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`,
		sub.UserID, sub.Asset, sub.Chain, sub.Address, sub.MinAmount, sub.Channel,
	).Scan(&sub.ID, &sub.CreatedAt)
}

func (s *Store) DeleteSubscription(ctx context.Context, userID string, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM notification_subscriptions WHERE user_id=$1 AND id=$2`, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) ListSubscriptions(ctx context.Context, userID string) ([]*Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, asset, chain, address_filter, min_amount, channel, active, created_at
		FROM notification_subscriptions WHERE user_id=$1 ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		sub := &Subscription{}
		if err := rows.Scan(&sub.ID, &sub.UserID, &sub.Asset, &sub.Chain, &sub.Address, &sub.MinAmount,
			&sub.Channel, &sub.Active, &sub.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// MatchDepositSubs returns every active subscription for uid that matches the
// (asset, chain, address, amount) tuple of a newly-completed deposit.
func (s *Store) MatchDepositSubs(ctx context.Context, uid, asset, chain, address string, amount decimal.Decimal) ([]*Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, asset, chain, address_filter, min_amount, channel, active, created_at
		FROM notification_subscriptions
		WHERE active=TRUE AND user_id=$1 AND asset=$2
		  AND (chain='' OR chain=$3)
		  AND (address_filter='' OR address_filter=$4)
		  AND (min_amount IS NULL OR min_amount <= $5)`,
		uid, asset, chain, address, amount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		sub := &Subscription{}
		if err := rows.Scan(&sub.ID, &sub.UserID, &sub.Asset, &sub.Chain, &sub.Address, &sub.MinAmount,
			&sub.Channel, &sub.Active, &sub.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// ---------- cursors ----------

func (s *Store) Cursor(ctx context.Context, name string) (int64, error) {
	var pos int64
	err := s.db.QueryRowContext(ctx, `SELECT position FROM report_cursors WHERE name=$1`, name).Scan(&pos)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return pos, err
}

func (s *Store) SetCursor(ctx context.Context, name string, pos int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO report_cursors (name, position, updated_at) VALUES ($1,$2,now())
		ON CONFLICT (name) DO UPDATE SET position=EXCLUDED.position, updated_at=now()
		WHERE report_cursors.position < EXCLUDED.position`, name, pos)
	return err
}
