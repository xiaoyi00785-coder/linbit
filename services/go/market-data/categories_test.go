package main

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

func TestFilterCategories(t *testing.T) {
	cats := []Category{{ID: "layer1", Name: "公链", Symbols: []string{"BTCUSDT", "SOLUSDT", "XRPUSDT"}}, {ID: "metals", Name: "贵金属", Symbols: []string{}}}
	got := filterCategories(cats, map[string]bool{"BTCUSDT": true, "SOLUSDT": true})
	if len(got) != 2 || len(got[0].Symbols) != 2 || got[0].Symbols[1] != "SOLUSDT" {
		t.Fatalf("layer1 = %+v", got)
	}
	if got[1].Symbols == nil || len(got[1].Symbols) != 0 {
		t.Fatalf("metals must be empty list, got %#v", got[1].Symbols)
	}
}

// TestCategoryMigrationsOrder checks the seeded ids/sort across migrations.
func TestCategoryMigrationsOrder(t *testing.T) {
	want := []string{"layer1", "metals", "commodities", "stocks", "meme", "ai", "defi", "layer2", "ecosystem",
		"solana", "gamefi", "depin", "nft", "ton", "lsd", "web3", "platform", "payment"}
	re := regexp.MustCompile(`\('([a-z0-9]+)',\s*'[^']*',\s*(\d+),`)
	type row struct {
		id   string
		sort int
	}
	var rows []row
	for _, f := range []string{"migrations/00002_categories.sql", "migrations/00003_categories_more.sql"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			n, _ := strconv.Atoi(m[2])
			rows = append(rows, row{m[1], n})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].sort < rows[j].sort })
	if len(rows) != len(want) {
		t.Fatalf("got %d categories, want %d: %+v", len(rows), len(want), rows)
	}
	for i, r := range rows {
		if r.id != want[i] {
			t.Fatalf("pos %d = %s, want %s", i, r.id, want[i])
		}
	}
}
