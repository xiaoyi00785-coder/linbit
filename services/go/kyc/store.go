package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned when a row lookup misses.
var ErrNotFound = errors.New("not found")

// ErrPendingExists is returned when a user already has a pending application.
var ErrPendingExists = errors.New("pending application exists")

// ErrIDNumberTaken is returned when the identity document is already approved for
// another user.
var ErrIDNumberTaken = errors.New("id number already in use")

// Application statuses. These are also the openapi KycStatus enum values (plus
// "none" when the user has no application at all).
const (
	statusPending  = "pending"
	statusApproved = "approved"
	statusRejected = "rejected"
	statusNone     = "none"
)

// Document sides.
const (
	sideFront  = "front"
	sideSelfie = "selfie"
	sideBack   = "back"
)

// Store wraps the KYC database.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Document is an uploaded KYC document owned by a user.
type Document struct {
	ID          int64
	UserID      string
	Side        string
	FileKey     string
	ContentType string
	SizeBytes   int64
	CreatedAt   time.Time
}

// Application is a KYC submission and its review-state row.
type Application struct {
	ID          int64
	UserID      string
	Level       int
	Status      string
	FirstName   string
	LastName    string
	Country     string
	IDType      string
	IDNumber    string
	DateOfBirth time.Time
	FrontDocID  sql.NullInt64
	SelfieDocID sql.NullInt64
	BackDocID   sql.NullInt64
	Reason      sql.NullString
	SubmittedAt time.Time
	ReviewedAt  sql.NullTime
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ---- documents ----

// InsertDocument stores an uploaded document's metadata and returns its id.
func (s *Store) InsertDocument(ctx context.Context, d Document) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO kyc_documents (user_id, side, file_key, content_type, size_bytes)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		d.UserID, d.Side, d.FileKey, d.ContentType, d.SizeBytes).Scan(&id)
	return id, err
}

// GetDocument fetches a document by id.
func (s *Store) GetDocument(ctx context.Context, id int64) (*Document, error) {
	var d Document
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, side, file_key, content_type, size_bytes, created_at
		 FROM kyc_documents WHERE id = $1`, id).
		Scan(&d.ID, &d.UserID, &d.Side, &d.FileKey, &d.ContentType, &d.SizeBytes, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// OwnedDocument fetches a document only if it belongs to userID (ownership gate).
func (s *Store) OwnedDocument(ctx context.Context, userID string, id int64) (*Document, error) {
	d, err := s.GetDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.UserID != userID {
		return nil, ErrNotFound
	}
	return d, nil
}

// ---- applications ----

const appColumns = `id, user_id, level, status, first_name, last_name, country,
	id_type, id_number, date_of_birth, front_doc_id, selfie_doc_id, back_doc_id,
	reason, submitted_at, reviewed_at, created_at, updated_at`

func scanApplication(row interface{ Scan(...any) error }) (*Application, error) {
	var a Application
	err := row.Scan(&a.ID, &a.UserID, &a.Level, &a.Status, &a.FirstName, &a.LastName, &a.Country,
		&a.IDType, &a.IDNumber, &a.DateOfBirth, &a.FrontDocID, &a.SelfieDocID, &a.BackDocID,
		&a.Reason, &a.SubmittedAt, &a.ReviewedAt, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// InsertApplication persists a pending application. It maps the "one pending" and
// "id number already approved" unique-index violations onto typed errors so the
// handler can return clean business codes (and these also guard against races).
func (s *Store) InsertApplication(ctx context.Context, a Application) (*Application, error) {
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO kyc_applications
		   (user_id, level, status, first_name, last_name, country, id_type, id_number,
		    date_of_birth, front_doc_id, selfie_doc_id, back_doc_id, submitted_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, now())
		 RETURNING `+appColumns,
		a.UserID, a.Level, statusPending, a.FirstName, a.LastName, a.Country, a.IDType,
		a.IDNumber, a.DateOfBirth, a.FrontDocID, a.SelfieDocID, a.BackDocID)
	out, err := scanApplication(row)
	if err != nil {
		switch uniqueConstraint(err) {
		case "uq_kyc_one_pending":
			return nil, ErrPendingExists
		case "uq_kyc_id_number_approved":
			return nil, ErrIDNumberTaken
		}
		return nil, err
	}
	return out, nil
}

// HighestApprovedLevel returns the user's highest approved KYC level (0 if none).
func (s *Store) HighestApprovedLevel(ctx context.Context, userID string) (int, error) {
	var lvl sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT max(level) FROM kyc_applications WHERE user_id = $1 AND status = 'approved'`,
		userID).Scan(&lvl)
	if err != nil {
		return 0, err
	}
	return int(lvl.Int64), nil
}

// HasPending reports whether the user currently has a pending application.
func (s *Store) HasPending(ctx context.Context, userID string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM kyc_applications WHERE user_id = $1 AND status = 'pending' LIMIT 1`,
		userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// IDNumberApprovedByOther reports whether the (country, idType, idNumber) is
// already approved for a DIFFERENT user.
func (s *Store) IDNumberApprovedByOther(ctx context.Context, userID, country, idType, idNumber string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM kyc_applications
		 WHERE status = 'approved' AND country = $1 AND id_type = $2 AND id_number = $3
		   AND user_id <> $4 LIMIT 1`,
		country, idType, idNumber, userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// LatestApplication returns the user's most recent application (ErrNotFound if
// the user has never submitted).
func (s *Store) LatestApplication(ctx context.Context, userID string) (*Application, error) {
	return scanApplication(s.db.QueryRowContext(ctx,
		`SELECT `+appColumns+` FROM kyc_applications WHERE user_id = $1
		 ORDER BY created_at DESC, id DESC LIMIT 1`, userID))
}

// GetApplication fetches an application by id.
func (s *Store) GetApplication(ctx context.Context, id int64) (*Application, error) {
	return scanApplication(s.db.QueryRowContext(ctx,
		`SELECT `+appColumns+` FROM kyc_applications WHERE id = $1`, id))
}

// ListApplications returns up to limit applications, optionally filtered by
// status, newest first.
func (s *Store) ListApplications(ctx context.Context, status string, limit int) ([]Application, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if status == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT `+appColumns+` FROM kyc_applications ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT `+appColumns+` FROM kyc_applications WHERE status = $1
			 ORDER BY created_at DESC, id DESC LIMIT $2`, status, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Application
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// Approve moves a pending application to approved. Returns true if it applied
// (false if it was not pending). The unique id-number index may reject the update
// if the same document was approved for another user in the meantime.
func (s *Store) Approve(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE kyc_applications
		   SET status = 'approved', reviewed_at = now(), updated_at = now()
		 WHERE id = $1 AND status = 'pending'`, id)
	if err != nil {
		if uniqueConstraint(err) == "uq_kyc_id_number_approved" {
			return false, ErrIDNumberTaken
		}
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Reject moves a pending application to rejected with a reason. Returns true if
// it applied.
func (s *Store) Reject(ctx context.Context, id int64, reason string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE kyc_applications
		   SET status = 'rejected', reason = $2, reviewed_at = now(), updated_at = now()
		 WHERE id = $1 AND status = 'pending'`, id, reason)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// uniqueConstraint returns the violated unique-constraint name for a Postgres
// unique_violation error, or "" otherwise.
func uniqueConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return strings.ToLower(pgErr.ConstraintName)
	}
	return ""
}
