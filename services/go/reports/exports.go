package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

// exportKinds defines every allowed report source; a user-supplied kind that is
// not in this map is rejected up front.
var exportKinds = map[string]bool{
	"trades":      true,
	"deposits":    true,
	"withdrawals": true,
	"transfers":   true,
	"funding":     true,
	"ledger":      true,
}

var exportFormats = map[string]bool{"csv": true, "xlsx": true, "pdf": true}

// safeJobIDRe keeps a jobId down to the chars we generate (hex) so it is safe
// to use as a filesystem segment under ExportsDir.
var safeJobIDRe = regexp.MustCompile(`^[a-f0-9]{16,64}$`)

type createExportReq struct {
	Type   string `json:"type"`
	From   string `json:"from"`
	To     string `json:"to"`
	Format string `json:"format"`
}

type exportDTO struct {
	JobID       string  `json:"jobId"`
	Type        string  `json:"type"`
	Format      string  `json:"format"`
	Status      string  `json:"status"`
	From        *int64  `json:"from,omitempty"`
	To          *int64  `json:"to,omitempty"`
	RowCount    *int    `json:"rowCount,omitempty"`
	SizeBytes   *int64  `json:"sizeBytes,omitempty"`
	DownloadURL string  `json:"downloadUrl,omitempty"`
	ExpiresAt   *int64  `json:"expiresAt,omitempty"`
	CreatedAt   int64   `json:"createdAt"`
	Error       string  `json:"error,omitempty"`
}

func (s *Server) toDTO(j *ExportJob) exportDTO {
	d := exportDTO{
		JobID:     j.JobID,
		Type:      j.Kind,
		Format:    j.Format,
		Status:    j.Status,
		CreatedAt: j.CreatedAt.UnixMilli(),
	}
	if j.FromAt.Valid {
		v := j.FromAt.Time.UnixMilli()
		d.From = &v
	}
	if j.ToAt.Valid {
		v := j.ToAt.Time.UnixMilli()
		d.To = &v
	}
	if j.RowCount.Valid {
		v := int(j.RowCount.Int32)
		d.RowCount = &v
	}
	if j.SizeBytes.Valid {
		v := j.SizeBytes.Int64
		d.SizeBytes = &v
	}
	if j.ExpiresAt.Valid {
		v := j.ExpiresAt.Time.UnixMilli()
		d.ExpiresAt = &v
	}
	if j.Error.Valid {
		d.Error = j.Error.String
	}
	if j.Status == "ready" && j.DownloadToken.Valid {
		d.DownloadURL = fmt.Sprintf("%s/%s/download?token=%s",
			s.cfg.DownloadBaseURL, j.JobID, j.DownloadToken.String)
	}
	return d
}

// POST /reports/exports
func (s *Server) handleCreateExport(w http.ResponseWriter, r *http.Request, uid string) {
	var req createExportReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求格式错误"))
		return
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if !exportKinds[req.Type] {
		writeErr(w, bad("VALIDATION_ERROR", "不支持的报表类型"))
		return
	}
	if req.Format == "" {
		req.Format = "csv"
	}
	if !exportFormats[req.Format] {
		writeErr(w, bad("VALIDATION_ERROR", "不支持的导出格式"))
		return
	}
	if req.Format == "pdf" && req.Type != "trades" && req.Type != "ledger" {
		// PDF is reserved for the summary layouts (trades / ledger) and the
		// dedicated tax report. Table-only reports must use CSV / XLSX.
		writeErr(w, bad("VALIDATION_ERROR", "该报表仅支持 CSV/XLSX 导出"))
		return
	}

	j := &ExportJob{
		JobID:  randomID(),
		UserID: uid,
		Kind:   req.Type,
		Format: req.Format,
	}
	if t, ok := parseTime(req.From); ok {
		j.FromAt = sql.NullTime{Time: t, Valid: true}
	}
	if t, ok := parseTime(req.To); ok {
		j.ToAt = sql.NullTime{Time: t, Valid: true}
	}
	if j.FromAt.Valid && j.ToAt.Valid && j.ToAt.Time.Before(j.FromAt.Time) {
		writeErr(w, bad("VALIDATION_ERROR", "时间区间无效"))
		return
	}
	if err := s.store.InsertExport(r.Context(), j); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusAccepted, s.toDTO(j))
}

// GET /reports/exports
func (s *Server) handleListExports(w http.ResponseWriter, r *http.Request, uid string) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	jobs, err := s.store.ListExports(r.Context(), uid, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	items := make([]exportDTO, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, s.toDTO(j))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /reports/exports/{jobId}
func (s *Server) handleGetExport(w http.ResponseWriter, r *http.Request, uid string) {
	jobID := r.PathValue("jobId")
	j, err := s.store.GetExport(r.Context(), uid, jobID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if j == nil {
		writeErr(w, notFound("NOT_FOUND", "任务不存在"))
		return
	}
	common.WriteJSON(w, http.StatusOK, s.toDTO(j))
}

// GET /reports/exports/{jobId}/download?token=...
// Token-authenticated — no X-User-Id needed, mirrors avatar.go's download route.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobId")
	token := r.URL.Query().Get("token")
	if token == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "缺少下载凭证")
		return
	}
	j, err := s.store.GetExportByJobID(r.Context(), jobID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if j == nil || !j.DownloadToken.Valid || j.DownloadToken.String != token {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "下载链接无效或已失效")
		return
	}
	if j.Status != "ready" || !j.FilePath.Valid {
		common.WriteError(w, http.StatusConflict, "NOT_READY", "报表尚未生成")
		return
	}
	if j.ExpiresAt.Valid && time.Now().After(j.ExpiresAt.Time) {
		common.WriteError(w, http.StatusGone, "EXPIRED", "下载链接已过期")
		return
	}
	f, err := os.Open(j.FilePath.String)
	if err != nil {
		common.WriteError(w, http.StatusGone, "EXPIRED", "报表文件已失效")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType(j.Format))
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s_%s.%s"`, j.Kind, j.JobID, j.Format))
	_, _ = io.Copy(w, f)
}

func contentType(format string) string {
	switch format {
	case "csv":
		return "text/csv; charset=utf-8"
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "pdf":
		return "application/pdf"
	}
	return "application/octet-stream"
}

// RunExportWorker drains queued export jobs one at a time. One worker per
// process is enough: Postgres SKIP LOCKED makes it safe to scale out later.
func (s *Server) RunExportWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		j, err := s.store.LeaseNextExport(ctx)
		if err != nil {
			log.Printf("reports: lease export: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if j == nil {
			time.Sleep(2 * time.Second)
			continue
		}
		s.runExport(ctx, j)
	}
}

func (s *Server) runExport(ctx context.Context, j *ExportJob) {
	path, rows, size, err := s.generate(ctx, j)
	if err != nil {
		if ferr := s.store.FailExport(ctx, j.ID, truncate(err.Error(), 400)); ferr != nil {
			log.Printf("reports: fail export %s: %v", j.JobID, ferr)
		}
		return
	}
	token := randomID()
	expires := time.Now().Add(s.cfg.ExportsTTL)
	if err := s.store.CompleteExport(ctx, j.ID, path, token, rows, size, expires); err != nil {
		log.Printf("reports: complete export %s: %v", j.JobID, err)
	}
}

// generate writes the file for j to disk and returns (path, rowCount, size).
// It owns the file: on error, any partial file is removed.
func (s *Server) generate(ctx context.Context, j *ExportJob) (string, int, int64, error) {
	if !safeJobIDRe.MatchString(j.JobID) {
		return "", 0, 0, errors.New("invalid jobId")
	}
	if strings.HasPrefix(j.Kind, "tax:") {
		return s.generateTax(ctx, j)
	}
	userDir := filepath.Join(s.cfg.ExportsDir, sanitize(j.UserID))
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return "", 0, 0, err
	}
	path := filepath.Join(userDir, j.JobID+"."+j.Format)
	cols, rows, err := s.fetch(ctx, j)
	if err != nil {
		return "", 0, 0, err
	}
	switch j.Format {
	case "csv":
		err = writeCSV(path, cols, rows)
	case "xlsx":
		err = writeXLSX(path, j.Kind, cols, rows)
	case "pdf":
		err = writeSummaryPDF(path, j.Kind, j.UserID, cols, rows, j.FromAt, j.ToAt)
	default:
		err = fmt.Errorf("unsupported format %q", j.Format)
	}
	if err != nil {
		_ = os.Remove(path)
		return "", 0, 0, err
	}
	fi, serr := os.Stat(path)
	if serr != nil {
		return "", 0, 0, serr
	}
	return path, len(rows), fi.Size(), nil
}

// fetch runs the read query for one export kind. The row shape is uniform:
// a []string header + slice of []string rows, so every writer can handle it.
func (s *Server) fetch(ctx context.Context, j *ExportJob) ([]string, [][]string, error) {
	from, to := timeRange(j.FromAt, j.ToAt)
	switch j.Kind {
	case "trades":
		return s.fetchTrades(ctx, j.UserID, from, to)
	case "deposits":
		return s.fetchDeposits(ctx, j.UserID, from, to)
	case "withdrawals":
		return s.fetchWithdrawals(ctx, j.UserID, from, to)
	case "transfers":
		return s.fetchTransfers(ctx, j.UserID, from, to)
	case "funding":
		return s.fetchFunding(ctx, j.UserID, from, to)
	case "ledger":
		return s.fetchLedger(ctx, j.UserID, from, to)
	}
	return nil, nil, fmt.Errorf("unknown kind %q", j.Kind)
}

// timeRange collapses the user-supplied window into a (from, to) pair with
// defaults; `from = epoch` and `to = now + 1h` so Postgres bounds every query.
func timeRange(fromAt, toAt sql.NullTime) (time.Time, time.Time) {
	from := time.Unix(0, 0)
	to := time.Now().Add(time.Hour)
	if fromAt.Valid {
		from = fromAt.Time
	}
	if toAt.Valid {
		to = toAt.Time
	}
	return from, to
}

func (s *Server) fetchTrades(ctx context.Context, uid string, from, to time.Time) ([]string, [][]string, error) {
	cols := []string{"time", "symbol", "side", "role", "price", "quantity", "quote", "fee", "fee_asset", "order_id", "trade_id"}
	rows, err := s.db.QueryContext(ctx, `
		SELECT created_at, symbol, side, role, price, quantity, fee, fee_asset, order_id, trade_id
		FROM trades WHERE user_id=$1 AND created_at BETWEEN $2 AND $3
		ORDER BY created_at ASC LIMIT $4`, uid, from, to, s.cfg.MaxExportRows)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var at time.Time
		var symbol, side, role, orderID, tradeID string
		var price, qty, fee decimal.Decimal
		var feeAsset sql.NullString
		if err := rows.Scan(&at, &symbol, &side, &role, &price, &qty, &fee, &feeAsset, &orderID, &tradeID); err != nil {
			return nil, nil, err
		}
		quote := price.Mul(qty)
		out = append(out, []string{
			at.UTC().Format(time.RFC3339), symbol, side, role,
			price.String(), qty.String(), quote.String(),
			fee.String(), feeAsset.String, orderID, tradeID,
		})
	}
	return cols, out, rows.Err()
}

func (s *Server) fetchDeposits(ctx context.Context, uid string, from, to time.Time) ([]string, [][]string, error) {
	cols := []string{"time", "asset", "network", "address", "amount", "tx_hash", "confirmations", "status"}
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(credited_at, created_at), asset, network, address, amount, tx_hash, confirmations, status
		FROM deposits WHERE user_id=$1 AND COALESCE(credited_at, created_at) BETWEEN $2 AND $3
		ORDER BY COALESCE(credited_at, created_at) ASC LIMIT $4`, uid, from, to, s.cfg.MaxExportRows)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var at time.Time
		var asset, network, address, txHash, status string
		var amount decimal.Decimal
		var conf int64
		if err := rows.Scan(&at, &asset, &network, &address, &amount, &txHash, &conf, &status); err != nil {
			return nil, nil, err
		}
		out = append(out, []string{at.UTC().Format(time.RFC3339), asset, network, address,
			amount.String(), txHash, strconv.FormatInt(conf, 10), status})
	}
	return cols, out, rows.Err()
}

func (s *Server) fetchWithdrawals(ctx context.Context, uid string, from, to time.Time) ([]string, [][]string, error) {
	cols := []string{"time", "asset", "network", "address", "amount", "gas_fee", "gas_fee_asset", "status", "tx_hash"}
	rows, err := s.db.QueryContext(ctx, `
		SELECT created_at, asset, network, address, amount, gas_fee, gas_fee_asset, status, tx_hash
		FROM withdrawals WHERE user_id=$1 AND created_at BETWEEN $2 AND $3
		ORDER BY created_at ASC LIMIT $4`, uid, from, to, s.cfg.MaxExportRows)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var at time.Time
		var asset, network, address, status, gasAsset string
		var amount, gas decimal.Decimal
		var txHash sql.NullString
		if err := rows.Scan(&at, &asset, &network, &address, &amount, &gas, &gasAsset, &status, &txHash); err != nil {
			return nil, nil, err
		}
		out = append(out, []string{at.UTC().Format(time.RFC3339), asset, network, address,
			amount.String(), gas.String(), gasAsset, status, txHash.String})
	}
	return cols, out, rows.Err()
}

func (s *Server) fetchTransfers(ctx context.Context, uid string, from, to time.Time) ([]string, [][]string, error) {
	cols := []string{"time", "direction", "counterparty", "asset", "amount", "value_usdt", "status", "client_id"}
	rows, err := s.db.QueryContext(ctx, `
		SELECT created_at, user_id, to_user_id, asset, amount, value_usdt, status, client_id
		FROM internal_transfers WHERE (user_id=$1 OR to_user_id=$1) AND created_at BETWEEN $2 AND $3
		ORDER BY created_at ASC LIMIT $4`, uid, from, to, s.cfg.MaxExportRows)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var at time.Time
		var fromUID, toUID, asset, status, clientID string
		var amount, value decimal.Decimal
		if err := rows.Scan(&at, &fromUID, &toUID, &asset, &amount, &value, &status, &clientID); err != nil {
			return nil, nil, err
		}
		dir := "out"
		cp := toUID
		if toUID == uid {
			dir = "in"
			cp = fromUID
		}
		out = append(out, []string{at.UTC().Format(time.RFC3339), dir, cp, asset,
			amount.String(), value.String(), status, clientID})
	}
	return cols, out, rows.Err()
}

func (s *Server) fetchFunding(ctx context.Context, uid string, from, to time.Time) ([]string, [][]string, error) {
	// funding_balances tracks the user's funding-account snapshot (not a
	// time-series); we export the current balances in that account. from/to
	// are ignored for this kind.
	_ = from
	_ = to
	cols := []string{"asset", "available", "frozen"}
	rows, err := s.db.QueryContext(ctx, `
		SELECT asset, available, frozen FROM funding_balances WHERE user_id=$1
		ORDER BY asset ASC`, uid)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var asset string
		var avail, frozen decimal.Decimal
		if err := rows.Scan(&asset, &avail, &frozen); err != nil {
			return nil, nil, err
		}
		out = append(out, []string{asset, avail.String(), frozen.String()})
	}
	return cols, out, rows.Err()
}

func (s *Server) fetchLedger(ctx context.Context, uid string, from, to time.Time) ([]string, [][]string, error) {
	cols := []string{"time", "tx_type", "ref", "account", "asset", "direction", "amount"}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.created_at, t.tx_type, t.ref, e.account, e.asset, e.direction, e.amount
		FROM ledger_transactions t
		JOIN ledger_entries e ON e.tx_id = t.id
		WHERE t.user_id=$1 AND t.created_at BETWEEN $2 AND $3
		ORDER BY t.created_at ASC, e.id ASC LIMIT $4`, uid, from, to, s.cfg.MaxExportRows)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var at time.Time
		var ref sql.NullString
		var txType, account, asset, direction string
		var amount decimal.Decimal
		if err := rows.Scan(&at, &txType, &ref, &account, &asset, &direction, &amount); err != nil {
			return nil, nil, err
		}
		out = append(out, []string{at.UTC().Format(time.RFC3339), txType, ref.String,
			account, asset, direction, amount.String()})
	}
	return cols, out, rows.Err()
}

// ---------- writers ----------

// writeCSV is a UTF-8 CSV with a BOM so Excel on Windows picks CJK correctly.
func writeCSV(path string, cols []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.Write(cols); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(r); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writeXLSX(path, sheetName string, cols []string, rows [][]string) error {
	f := excelize.NewFile()
	defer f.Close()
	sheet := strings.Title(sheetName)
	if sheet == "" {
		sheet = "Sheet1"
	}
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return err
	}
	f.SetActiveSheet(idx)
	// Drop the default blank sheet so there is no empty "Sheet1" tab next to
	// the data when sheetName itself was "sheet1" in a different case.
	if sheet != "Sheet1" {
		_ = f.DeleteSheet("Sheet1")
	}
	for i, c := range cols {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, c); err != nil {
			return err
		}
	}
	for ri, row := range rows {
		for ci, v := range row {
			cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				return err
			}
		}
	}
	return f.SaveAs(path)
}

// ---------- helpers ----------

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	// Accept unix ms or RFC3339.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMilli(n), true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}

var sanitizeRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func sanitize(s string) string { return sanitizeRe.ReplaceAllString(s, "_") }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// expireOnce marks expired ready jobs and removes their files from disk. Any
// file-removal failure is logged but never blocks the next pass.
func (s *Server) expireOnce(ctx context.Context) {
	paths, err := s.store.ExpireReady(ctx)
	if err != nil {
		log.Printf("reports: expire: %v", err)
		return
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("reports: remove %s: %v", p, err)
		}
	}
}
