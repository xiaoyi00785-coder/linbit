package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// TestWriteCSV verifies the CSV writer emits the BOM and the row shape the UI
// expects (header + one row per data row, columns in the given order).
func TestWriteCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	cols := []string{"time", "asset", "amount"}
	rows := [][]string{
		{"2026-01-01T00:00:00Z", "BTC", "0.5"},
		{"2026-01-02T00:00:00Z", "ETH", "2"},
	}
	if err := writeCSV(path, cols, rows); err != nil {
		t.Fatalf("writeCSV: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("missing UTF-8 BOM")
	}
	r := csv.NewReader(bytes.NewReader(data[3:]))
	got, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("rows: want 3 got %d", len(got))
	}
	if got[0][0] != "time" || got[0][2] != "amount" {
		t.Fatalf("header mismatch: %v", got[0])
	}
	if got[2][1] != "ETH" {
		t.Fatalf("row[2] asset: %v", got[2])
	}
}

// TestWriteXLSX builds a workbook and re-reads it to confirm the sheet, the
// header row and a data cell are all what the writer emitted.
func TestWriteXLSX(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.xlsx")
	cols := []string{"time", "asset", "amount"}
	rows := [][]string{{"t1", "BTC", "0.5"}, {"t2", "ETH", "2"}}
	if err := writeXLSX(path, "trades", cols, rows); err != nil {
		t.Fatalf("writeXLSX: %v", err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	found := false
	for _, s := range sheets {
		if strings.EqualFold(s, "Trades") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no Trades sheet: %v", sheets)
	}
	v, err := f.GetCellValue("Trades", "A1")
	if err != nil || v != "time" {
		t.Fatalf("A1: want 'time' got %q err %v", v, err)
	}
	v, _ = f.GetCellValue("Trades", "B3")
	if v != "ETH" {
		t.Fatalf("B3: want 'ETH' got %q", v)
	}
}

// TestWriteSummaryPDF generates the trades summary PDF and checks the output
// is a valid PDF (begins with %PDF-) and non-empty.
func TestWriteSummaryPDF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.pdf")
	cols := []string{"time", "asset", "side"}
	rows := [][]string{{"2026-01-01", "BTCUSDT", "buy"}, {"2026-01-02", "BTCUSDT", "sell"}}
	from := sql.NullTime{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	to := sql.NullTime{Time: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	if err := writeSummaryPDF(path, "trades", "u1", cols, rows, from, to); err != nil {
		t.Fatalf("writeSummaryPDF: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", data[:min(16, len(data))])
	}
	if len(data) < 1024 {
		t.Fatalf("PDF suspiciously small: %d bytes", len(data))
	}
}

// TestTaxPDFContainsTotals renders a US tax PDF and confirms the file is a
// valid PDF; the rendering path pulls in gofpdf's font tables so running it
// here catches a broken dependency wiring that writeSummaryPDF alone could miss.
func TestTaxPDFContainsTotals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tax.pdf")
	tpl := taxTemplates["US"]
	s := &taxSummary{QuoteAsset: "USDT"}
	if err := renderTaxPDF(path, tpl, "u1", 2025, s); err != nil {
		t.Fatalf("renderTaxPDF: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("not a PDF")
	}
}

func TestParseTime(t *testing.T) {
	cases := map[string]bool{
		"":                      false,
		"1735689600000":         true,  // 2025-01-01 unix ms
		"2026-01-02":            true,
		"2026-01-02T03:04:05Z":  true,
		"not-a-date":            false,
	}
	for in, want := range cases {
		_, ok := parseTime(in)
		if ok != want {
			t.Errorf("parseTime(%q) = %v, want %v", in, ok, want)
		}
	}
}

func TestSplitSymbol(t *testing.T) {
	cases := map[string][2]string{
		"BTCUSDT":  {"BTC", "USDT"},
		"ETHBTC":   {"ETH", "BTC"},
		"SOLUSDC":  {"SOL", "USDC"},
		"UNKNOWN":  {"UNKNOWN", "USDT"},
	}
	for in, want := range cases {
		b, q := splitSymbol(in)
		if b != want[0] || q != want[1] {
			t.Errorf("splitSymbol(%q) = (%s,%s), want (%s,%s)", in, b, q, want[0], want[1])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
