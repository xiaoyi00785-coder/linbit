package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// minPINLength is the client-enforced floor; we re-check it here so a direct
// curl can't skip it.
const minPINLength = 6

// ------------------------------------------------------------------ handlers

type createWalletReq struct {
	PIN       string `json:"pin"`
	Mnemonic  string `json:"mnemonic,omitempty"` // optional import
	Overwrite bool   `json:"overwrite,omitempty"`
}

type walletSummary struct {
	Exists       bool       `json:"exists"`
	CreatedAt    *time.Time `json:"createdAt,omitempty"`
	BackupAt     *time.Time `json:"backupAt,omitempty"`
	PINVersion   int        `json:"pinVersion,omitempty"`
	Locked       bool       `json:"locked"`
	LockedUntil  *time.Time `json:"lockedUntil,omitempty"`
	PINFailures  int        `json:"pinFailures"`
	Mnemonic     string     `json:"mnemonic,omitempty"` // returned ONLY on create, never after
	Chains       []string   `json:"chains"`
}

func (s *Server) handleCreateWallet(w http.ResponseWriter, r *http.Request, uid string) {
	var req createWalletReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	if err := validatePIN(req.PIN); err != nil {
		writeErr(w, err)
		return
	}
	lk := s.userLks.lock(uid)
	lk.Lock()
	defer lk.Unlock()

	ctx := r.Context()
	exists, err := s.walletExists(ctx, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if exists && !req.Overwrite {
		writeErr(w, conflict("WEB3_WALLET_EXISTS", "钱包已存在"))
		return
	}

	mnemonic := strings.TrimSpace(req.Mnemonic)
	if mnemonic == "" {
		mnemonic, err = NewMnemonic()
		if err != nil {
			writeErr(w, err)
			return
		}
	} else if err := ValidateMnemonic(mnemonic); err != nil {
		writeErr(w, bad("WEB3_BAD_MNEMONIC", "助记词无效"))
		return
	}

	derived, err := DeriveAll(mnemonic)
	if err != nil {
		writeErr(w, err)
		return
	}

	blob, err := EncryptMnemonic(mnemonic, req.PIN, s.cfg.KeystorePepper)
	if err != nil {
		writeErr(w, err)
		return
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback() //nolint:errcheck

	if exists {
		if _, err := tx.ExecContext(ctx,
			`UPDATE web3_wallets SET encrypted_mnemonic=$2, pin_version=pin_version+1, pin_failures=0, locked_until=NULL, backup_at=NULL, created_at=NOW() WHERE uid=$1`,
			uid, blob); err != nil {
			writeErr(w, err)
			return
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM web3_addresses WHERE uid=$1`, uid); err != nil {
			writeErr(w, err)
			return
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO web3_wallets (uid, encrypted_mnemonic) VALUES ($1,$2)`,
			uid, blob); err != nil {
			writeErr(w, err)
			return
		}
	}
	for _, d := range derived {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO web3_addresses (uid, chain, address, derivation_path) VALUES ($1,$2,$3,$4)`,
			uid, d.Chain, d.Address, d.Path); err != nil {
			writeErr(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}

	// The plaintext mnemonic is returned ONCE here (so the user can write it
	// down); any subsequent fetch returns only addresses.
	chains := make([]string, 0, len(derived))
	for _, d := range derived {
		chains = append(chains, d.Chain)
	}
	common.WriteJSON(w, http.StatusOK, walletSummary{
		Exists:   true,
		Chains:   chains,
		Mnemonic: mnemonic,
	})
}

func (s *Server) handleGetWallet(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	row := s.db.QueryRowContext(ctx,
		`SELECT created_at, backup_at, pin_version, pin_failures, locked_until FROM web3_wallets WHERE uid=$1`, uid)
	var createdAt time.Time
	var backupAt, lockedUntil sql.NullTime
	var pinVer, failures int
	if err := row.Scan(&createdAt, &backupAt, &pinVer, &failures, &lockedUntil); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			common.WriteJSON(w, http.StatusOK, walletSummary{Exists: false})
			return
		}
		writeErr(w, err)
		return
	}
	chains, err := s.listChains(ctx, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	sum := walletSummary{
		Exists:      true,
		CreatedAt:   &createdAt,
		PINVersion:  pinVer,
		PINFailures: failures,
		Chains:      chains,
	}
	if backupAt.Valid {
		sum.BackupAt = &backupAt.Time
	}
	if lockedUntil.Valid && lockedUntil.Time.After(time.Now()) {
		sum.Locked = true
		sum.LockedUntil = &lockedUntil.Time
	}
	common.WriteJSON(w, http.StatusOK, sum)
}

type changePINReq struct {
	OldPIN string `json:"oldPin"`
	NewPIN string `json:"newPin"`
}

func (s *Server) handleChangePIN(w http.ResponseWriter, r *http.Request, uid string) {
	var req changePINReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	if err := validatePIN(req.NewPIN); err != nil {
		writeErr(w, err)
		return
	}
	lk := s.userLks.lock(uid)
	lk.Lock()
	defer lk.Unlock()

	ctx := r.Context()
	mnemonic, err := s.unlockMnemonic(ctx, uid, req.OldPIN)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer func() { mnemonic = "" }()
	blob, err := EncryptMnemonic(mnemonic, req.NewPIN, s.cfg.KeystorePepper)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE web3_wallets SET encrypted_mnemonic=$2, pin_version=pin_version+1, pin_failures=0, locked_until=NULL WHERE uid=$1`,
		uid, blob); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type resetWalletReq struct {
	// Confirm is a free-form sentinel ("DELETE") the Flutter page requires the
	// user to type, to reduce the chance of a tap-through reset. The server
	// requires it exactly — this is defense in depth, not the primary control.
	Confirm string `json:"confirm"`
	// If NewMnemonic is empty a fresh one is generated; otherwise it's
	// validated and imported. Same contract as create-wallet.
	NewMnemonic string `json:"mnemonic,omitempty"`
	NewPIN      string `json:"pin"`
}

// handleResetWallet: user has lost their PIN but restored from cloud backup
// (or has the written-down mnemonic). Overwrites the server copy. The old
// pending tx history is kept — any funds still at the old addresses stay
// there, which is intentional if the user restored the SAME mnemonic.
func (s *Server) handleResetWallet(w http.ResponseWriter, r *http.Request, uid string) {
	var req resetWalletReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	if req.Confirm != "DELETE" {
		writeErr(w, bad("WEB3_CONFIRM_REQUIRED", "需要确认操作"))
		return
	}
	if err := validatePIN(req.NewPIN); err != nil {
		writeErr(w, err)
		return
	}
	// Delegate to createWallet with Overwrite: reset is semantically identical.
	r2 := r.Clone(r.Context())
	body := createWalletReq{PIN: req.NewPIN, Mnemonic: req.NewMnemonic, Overwrite: true}
	jsonInto(r2, body)
	s.handleCreateWallet(w, r2, uid)
}

type exportReq struct {
	PIN string `json:"pin"`
}

// handleExportMnemonic is the ONLY read path for the plaintext mnemonic after
// wallet creation. Rate-limited via the PIN lockout; not available over the
// public gateway if the caller skipped `requireSensitiveAuth` upstream.
func (s *Server) handleExportMnemonic(w http.ResponseWriter, r *http.Request, uid string) {
	var req exportReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	lk := s.userLks.lock(uid)
	lk.Lock()
	defer lk.Unlock()
	mnemonic, err := s.unlockMnemonic(r.Context(), uid, req.PIN)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"mnemonic": mnemonic})
}

func (s *Server) handleBackupAck(w http.ResponseWriter, r *http.Request, uid string) {
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE web3_wallets SET backup_at=NOW() WHERE uid=$1`, uid); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------- unlock helpers

// unlockMnemonic decrypts the user's mnemonic with the supplied PIN. On a bad
// PIN it bumps pin_failures; after MaxPINAttempts it sets locked_until and
// returns a locked error without ever revealing the correct PIN or byte count.
func (s *Server) unlockMnemonic(ctx context.Context, uid, pin string) (string, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT encrypted_mnemonic, pin_failures, locked_until FROM web3_wallets WHERE uid=$1`, uid)
	var blob string
	var failures int
	var lockedUntil sql.NullTime
	if err := row.Scan(&blob, &failures, &lockedUntil); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", notFound("WEB3_WALLET_NOT_FOUND", "尚未创建钱包")
		}
		return "", err
	}
	if lockedUntil.Valid && lockedUntil.Time.After(time.Now()) {
		return "", locked("WEB3_PIN_LOCKED", "错误过多，请稍后再试")
	}
	mnemonic, err := DecryptMnemonic(blob, pin, s.cfg.KeystorePepper)
	if err != nil {
		if errors.Is(err, ErrBadPIN) {
			if err2 := s.bumpPinFailure(ctx, uid, failures+1); err2 != nil {
				return "", err2
			}
			if failures+1 >= s.cfg.MaxPINAttempts {
				return "", locked("WEB3_PIN_LOCKED", "错误过多，请稍后再试")
			}
			return "", unauth("WEB3_PIN_WRONG", "PIN 错误")
		}
		return "", err
	}
	// Reset failure counter on success.
	_, _ = s.db.ExecContext(ctx, `UPDATE web3_wallets SET pin_failures=0, locked_until=NULL WHERE uid=$1`, uid)
	return mnemonic, nil
}

func (s *Server) bumpPinFailure(ctx context.Context, uid string, newCount int) error {
	if newCount >= s.cfg.MaxPINAttempts {
		until := time.Now().Add(time.Duration(s.cfg.LockMinutes) * time.Minute)
		_, err := s.db.ExecContext(ctx,
			`UPDATE web3_wallets SET pin_failures=$2, locked_until=$3 WHERE uid=$1`,
			uid, newCount, until)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE web3_wallets SET pin_failures=$2 WHERE uid=$1`, uid, newCount)
	return err
}

// ----------------------------------------------------------------- utilities

func (s *Server) walletExists(ctx context.Context, uid string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM web3_wallets WHERE uid=$1`, uid).Scan(&one)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (s *Server) listChains(ctx context.Context, uid string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT chain FROM web3_addresses WHERE uid=$1 ORDER BY chain`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func validatePIN(pin string) error {
	if len(pin) < minPINLength {
		return bad("WEB3_PIN_TOO_SHORT", "PIN 至少 6 位")
	}
	if len(pin) > 64 {
		return bad("WEB3_PIN_TOO_LONG", "PIN 过长")
	}
	return nil
}
