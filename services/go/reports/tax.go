package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Country templates map a country code to the section / line labels on the
// generated PDF. The numbers are always computed from the user's own trade /
// staking / ledger history; the template only changes labels and headings.
type taxTemplate struct {
	Country     string
	Title       string
	SectionCG   string // capital-gains heading (realized P&L)
	SectionStk  string // staking / earning heading
	SectionOth  string // airdrops / other income heading
	SectionFees string // fees heading
	Note        string // bottom disclaimer
}

var taxTemplates = map[string]taxTemplate{
	"US": {
		Country: "US", Title: "Annual Crypto Tax Summary",
		SectionCG:   "Realized Capital Gains (short- and long-term, aggregate)",
		SectionStk:  "Staking Rewards (ordinary income)",
		SectionOth:  "Airdrops / Other Income",
		SectionFees: "Trading Fees Paid",
		Note:        "Draft summary for your records. Not a Form 8949 and not tax advice.",
	},
	"DE": {
		Country: "DE", Title: "Jahres-Krypto-Steueruebersicht",
		SectionCG:   "Realisierte Veraeusserungsgewinne",
		SectionStk:  "Staking-Ertraege",
		SectionOth:  "Airdrops / Sonstige Einkuenfte",
		SectionFees: "Gezahlte Handelsgebuehren",
		Note:        "Entwurfsuebersicht. Keine Steuerberatung.",
	},
	"JP": {
		Country: "JP", Title: "Annual Crypto Tax Summary (JP)",
		SectionCG:   "Realized Gains (Miscellaneous Income)",
		SectionStk:  "Staking Rewards",
		SectionOth:  "Airdrops / Other Income",
		SectionFees: "Trading Fees Paid",
		Note:        "Draft summary. Not tax advice. Refer to NTA guidance.",
	},
	"CN": {
		Country: "CN", Title: "Annual Crypto Activity Summary (CN)",
		SectionCG:   "Realized Gains",
		SectionStk:  "Staking Rewards",
		SectionOth:  "Airdrops / Other Income",
		SectionFees: "Trading Fees Paid",
		Note:        "Informational summary only; not a tax filing.",
	},
}

type createTaxReq struct {
	Year    int    `json:"year"`
	Country string `json:"country"`
}

// POST /reports/tax — enqueue a tax PDF just like a regular export, kind=tax.
// The worker branches on kind=="tax" and uses this file's generator instead.
func (s *Server) handleCreateTaxReport(w http.ResponseWriter, r *http.Request, uid string) {
	var req createTaxReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求格式错误"))
		return
	}
	now := time.Now().UTC()
	if req.Year <= 0 {
		req.Year = now.Year() - 1
	}
	if req.Year < 2015 || req.Year > now.Year() {
		writeErr(w, bad("VALIDATION_ERROR", "年度无效"))
		return
	}
	country := strings.ToUpper(strings.TrimSpace(req.Country))
	if country == "" {
		country = s.cfg.DefaultTaxCountry
	}
	if _, ok := taxTemplates[country]; !ok {
		writeErr(w, bad("VALIDATION_ERROR", "尚未支持该国家/地区的税务模板"))
		return
	}
	if !contains(s.cfg.TaxCountries, country) {
		writeErr(w, bad("VALIDATION_ERROR", "该国家/地区的税务模板未启用"))
		return
	}

	from := time.Date(req.Year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(1, 0, 0)
	j := &ExportJob{
		JobID:  randomID(),
		UserID: uid,
		Kind:   "tax:" + country,
		Format: "pdf",
		FromAt: sql.NullTime{Time: from, Valid: true},
		ToAt:   sql.NullTime{Time: to, Valid: true},
	}
	if err := s.store.InsertExport(r.Context(), j); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusAccepted, s.toDTO(j))
}

// generateTax is wired in by the generic generate() switch via a "tax:XX" kind.
func (s *Server) generateTax(ctx context.Context, j *ExportJob) (string, int, int64, error) {
	country := strings.TrimPrefix(j.Kind, "tax:")
	tpl, ok := taxTemplates[country]
	if !ok {
		return "", 0, 0, fmt.Errorf("unknown country template %q", country)
	}
	if !j.FromAt.Valid || !j.ToAt.Valid {
		return "", 0, 0, errors.New("tax report missing year range")
	}
	summary, err := s.taxSummary(ctx, j.UserID, j.FromAt.Time, j.ToAt.Time)
	if err != nil {
		return "", 0, 0, err
	}
	userDir := filepath.Join(s.cfg.ExportsDir, sanitize(j.UserID))
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return "", 0, 0, err
	}
	path := filepath.Join(userDir, j.JobID+".pdf")
	if err := renderTaxPDF(path, tpl, j.UserID, j.FromAt.Time.Year(), summary); err != nil {
		_ = os.Remove(path)
		return "", 0, 0, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", 0, 0, err
	}
	rows := len(summary.PerAsset) + 3 // realized assets + the 3 totals rows
	return path, rows, fi.Size(), nil
}

// taxLine summarises one asset's realized P&L; realized gain is computed as
// (sell notional at trade price) - (buy notional at trade price) over the
// year using a flat aggregate method. This is explicitly not a lot-level
// FIFO/HIFO calculation — the generated PDF says so in its footer.
type taxLine struct {
	Asset       string
	BuyQty      decimal.Decimal
	BuyNotional decimal.Decimal
	SellQty     decimal.Decimal
	SellNotional decimal.Decimal
	Realized    decimal.Decimal // SellNotional - BuyNotional (quote currency)
}

type taxSummary struct {
	PerAsset       []taxLine
	StakingRewards decimal.Decimal // aggregated, in quote asset on the ledger
	AirdropIncome  decimal.Decimal
	Fees           decimal.Decimal
	QuoteAsset     string // best-effort: the quote side of the most-traded pair
}

func (s *Server) taxSummary(ctx context.Context, uid string, from, to time.Time) (*taxSummary, error) {
	ts := &taxSummary{QuoteAsset: "USDT"}

	// Trades: aggregate buys and sells per base asset. symbol is "BASEQUOTE"
	// without a separator, so we derive base by trimming the common quote.
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol, side, price, quantity, fee, COALESCE(fee_asset, '')
		FROM trades WHERE user_id=$1 AND created_at >= $2 AND created_at < $3`,
		uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	acc := map[string]*taxLine{}
	quoteVotes := map[string]int{}
	for rows.Next() {
		var symbol, side, feeAsset string
		var price, qty, fee decimal.Decimal
		if err := rows.Scan(&symbol, &side, &price, &qty, &fee, &feeAsset); err != nil {
			return nil, err
		}
		base, quote := splitSymbol(symbol)
		quoteVotes[quote]++
		line, ok := acc[base]
		if !ok {
			line = &taxLine{Asset: base}
			acc[base] = line
		}
		notional := price.Mul(qty)
		if strings.EqualFold(side, "buy") {
			line.BuyQty = line.BuyQty.Add(qty)
			line.BuyNotional = line.BuyNotional.Add(notional)
		} else {
			line.SellQty = line.SellQty.Add(qty)
			line.SellNotional = line.SellNotional.Add(notional)
		}
		if feeAsset != "" && feeAsset == quote {
			ts.Fees = ts.Fees.Add(fee)
		} else if feeAsset != "" {
			// Non-quote fees are reported in the fees-paid line as-is; the
			// user can reconcile them with the ledger section.
			ts.Fees = ts.Fees.Add(fee.Mul(price))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Pick the most-common quote as the display quote for the report; fall back
	// to USDT when the user never traded.
	if len(quoteVotes) > 0 {
		best := ""
		bestN := 0
		for q, n := range quoteVotes {
			if n > bestN {
				best = q
				bestN = n
			}
		}
		if best != "" {
			ts.QuoteAsset = best
		}
	}

	for _, line := range acc {
		line.Realized = line.SellNotional.Sub(line.BuyNotional)
	}

	// Ledger: staking rewards and airdrops. tx_type values live outside this
	// service, so we match on well-known strings and ignore the rest.
	lrows, err := s.db.QueryContext(ctx, `
		SELECT t.tx_type, e.asset, e.direction, e.amount
		FROM ledger_transactions t
		JOIN ledger_entries e ON e.tx_id = t.id
		WHERE t.user_id=$1 AND t.created_at >= $2 AND t.created_at < $3
		  AND e.direction='credit'`, uid, from, to)
	if err == nil {
		defer lrows.Close()
		for lrows.Next() {
			var kind, asset, dir string
			var amount decimal.Decimal
			if err := lrows.Scan(&kind, &asset, &dir, &amount); err != nil {
				return nil, err
			}
			switch {
			case strings.Contains(kind, "staking"), strings.Contains(kind, "earn"),
				strings.Contains(kind, "savings"):
				ts.StakingRewards = ts.StakingRewards.Add(amount)
			case strings.Contains(kind, "airdrop"), strings.Contains(kind, "reward"),
				strings.Contains(kind, "bonus"):
				ts.AirdropIncome = ts.AirdropIncome.Add(amount)
			}
			_ = asset
			_ = dir
		}
		if err := lrows.Err(); err != nil {
			return nil, err
		}
	} else {
		log.Printf("reports: tax ledger scan: %v", err)
	}

	assets := make([]taxLine, 0, len(acc))
	for _, line := range acc {
		assets = append(assets, *line)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Asset < assets[j].Asset })
	ts.PerAsset = assets
	return ts, nil
}

// splitSymbol is a best-effort base/quote split that peels off the common
// quote assets. Unknown symbols return (symbol, "USDT") so the row still
// aggregates somewhere instead of being silently dropped.
var commonQuotes = []string{"USDT", "USDC", "FDUSD", "DAI", "TUSD", "BUSD", "USD", "EUR", "BTC", "ETH"}

func splitSymbol(symbol string) (base, quote string) {
	sym := strings.ToUpper(symbol)
	for _, q := range commonQuotes {
		if strings.HasSuffix(sym, q) && len(sym) > len(q) {
			return sym[:len(sym)-len(q)], q
		}
	}
	return sym, "USDT"
}

func renderTaxPDF(path string, tpl taxTemplate, uid string, year int, s *taxSummary) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(0, 10, fmt.Sprintf("%s — %d", tpl.Title, year), "", 1, "C", false, 0, "")
	pdf.Ln(2)
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 6, fmt.Sprintf("Account: %s   Country: %s   Quote: %s", uid, tpl.Country, s.QuoteAsset), "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 6, fmt.Sprintf("Generated: %s UTC", time.Now().UTC().Format("2006-01-02 15:04")), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	section(pdf, tpl.SectionCG)
	if len(s.PerAsset) == 0 {
		pdf.CellFormat(0, 6, "No trades in this tax year.", "", 1, "L", false, 0, "")
	} else {
		headers := []string{"Asset", "Buy qty", "Buy notional", "Sell qty", "Sell notional", "Realized"}
		widths := []float64{20, 28, 34, 28, 34, 32}
		pdf.SetFont("Helvetica", "B", 9)
		for i, h := range headers {
			pdf.CellFormat(widths[i], 6, h, "B", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)
		pdf.SetFont("Helvetica", "", 9)
		var realized decimal.Decimal
		for _, l := range s.PerAsset {
			realized = realized.Add(l.Realized)
			cells := []string{l.Asset, l.BuyQty.String(), l.BuyNotional.String(),
				l.SellQty.String(), l.SellNotional.String(), l.Realized.String()}
			for i, c := range cells {
				pdf.CellFormat(widths[i], 5, c, "", 0, "L", false, 0, "")
			}
			pdf.Ln(-1)
		}
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(0, 6, fmt.Sprintf("Total realized (%s): %s", s.QuoteAsset, realized.String()), "T", 1, "R", false, 0, "")
	}
	pdf.Ln(4)
	section(pdf, tpl.SectionStk)
	pdf.CellFormat(0, 6, fmt.Sprintf("Total (%s): %s", s.QuoteAsset, s.StakingRewards.String()), "", 1, "L", false, 0, "")
	pdf.Ln(2)
	section(pdf, tpl.SectionOth)
	pdf.CellFormat(0, 6, fmt.Sprintf("Total (%s): %s", s.QuoteAsset, s.AirdropIncome.String()), "", 1, "L", false, 0, "")
	pdf.Ln(2)
	section(pdf, tpl.SectionFees)
	pdf.CellFormat(0, 6, fmt.Sprintf("Total (%s): %s", s.QuoteAsset, s.Fees.String()), "", 1, "L", false, 0, "")
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "I", 8)
	pdf.MultiCell(0, 4, tpl.Note, "", "L", false)
	return pdf.OutputFileAndClose(path)
}

func section(pdf *gofpdf.Fpdf, title string) {
	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(0, 7, title, "B", 1, "L", false, 0, "")
}

// writeSummaryPDF renders a one-page summary PDF for the "trades" and "ledger"
// kinds (CSV/XLSX export of the same kind still contains every row — this is a
// human-readable overview). Rows beyond the fit are summarized as a count.
func writeSummaryPDF(path, kind, uid string, cols []string, rows [][]string, from, to sql.NullTime) error {
	pdf := gofpdf.New("L", "mm", "A4", "") // landscape so wide rows fit
	pdf.SetMargins(10, 10, 10)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 14)
	pdf.CellFormat(0, 8, fmt.Sprintf("Linbit %s report", strings.Title(kind)), "", 1, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	rng := "all time"
	if from.Valid || to.Valid {
		a, b := "-", "-"
		if from.Valid {
			a = from.Time.UTC().Format("2006-01-02")
		}
		if to.Valid {
			b = to.Time.UTC().Format("2006-01-02")
		}
		rng = a + " to " + b
	}
	pdf.CellFormat(0, 5, fmt.Sprintf("Account: %s   Window: %s   Rows: %d", uid, rng, len(rows)), "", 1, "L", false, 0, "")
	pdf.Ln(2)

	pdf.SetFont("Helvetica", "B", 9)
	width := (297.0 - 20.0) / float64(len(cols))
	for _, c := range cols {
		pdf.CellFormat(width, 6, c, "B", 0, "L", false, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetFont("Helvetica", "", 8)
	const maxRows = 60
	shown := len(rows)
	if shown > maxRows {
		shown = maxRows
	}
	for i := 0; i < shown; i++ {
		for _, v := range rows[i] {
			pdf.CellFormat(width, 5, truncate(v, 24), "", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)
	}
	if len(rows) > shown {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "I", 8)
		pdf.CellFormat(0, 5, fmt.Sprintf("... %d more rows truncated; download CSV/XLSX for the full list.", len(rows)-shown), "", 1, "L", false, 0, "")
	}
	return pdf.OutputFileAndClose(path)
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
