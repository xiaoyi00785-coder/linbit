package main

import (
	"testing"
	"time"
)

func tk(sym, chg, qv string) tickerDTO {
	return tickerDTO{Symbol: sym, PriceChangePercent: chg, QuoteVolume: qv, LastPrice: "1"}
}

func syms(ts []tickerDTO) string {
	s := ""
	for _, t := range ts {
		s += t.Symbol + ","
	}
	return s
}

func TestRankHot(t *testing.T) {
	ts := []tickerDTO{tk("BTCUSDT", "1.5", "900"), tk("ETHUSDT", "-2", "500"), tk("SOLUSDT", "4", "100"), tk("TRXUSDT", "0", "50")}
	cases := []struct {
		typ  string
		pins []string
		want string
	}{
		{"hot", nil, "BTCUSDT,ETHUSDT,SOLUSDT,TRXUSDT,"},
		{"volume", nil, "BTCUSDT,ETHUSDT,SOLUSDT,TRXUSDT,"},
		{"gainers", nil, "SOLUSDT,BTCUSDT,"},
		{"losers", nil, "ETHUSDT,"},
		{"hot", []string{"TRXUSDT", "XXXUSDT", "TRXUSDT"}, "TRXUSDT,BTCUSDT,ETHUSDT,SOLUSDT,"},
		{"gainers", []string{"ETHUSDT"}, "ETHUSDT,SOLUSDT,BTCUSDT,"},
	}
	for _, c := range cases {
		if got := syms(rankHot(c.typ, ts, c.pins, nil, 0)); got != c.want {
			t.Errorf("%s pins=%v: got %s want %s", c.typ, c.pins, got, c.want)
		}
	}
	now := time.Now()
	first := map[string]time.Time{"BTCUSDT": now.Add(-48 * time.Hour), "ETHUSDT": now.Add(-24 * time.Hour)}
	if got := syms(rankHot("new", ts[:2], nil, first, 0)); got != "ETHUSDT,BTCUSDT," {
		t.Errorf("new: %s", got)
	}
	if got := syms(rankHot("new", ts, nil, first, 1)); got != "SOLUSDT," {
		t.Errorf("new limit (never traded first): %s", got)
	}
}

func TestPlacementVisible(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	base := Placement{Slot: "popup", Platform: "all", Audience: "all", Enabled: true, Regions: []string{}}
	q := placementQuery{Platform: "app", Locale: "zh-CN", AppVersion: "1.4.0", Region: "SG", Now: now}
	mod := func(f func(*Placement)) Placement { p := base; f(&p); return p }
	cases := []struct {
		name string
		p    Placement
		q    placementQuery
		want bool
	}{
		{"base", base, q, true},
		{"disabled", mod(func(p *Placement) { p.Enabled = false }), q, false},
		{"not started", mod(func(p *Placement) { p.StartAt = &future }), q, false},
		{"ended", mod(func(p *Placement) { p.EndAt = &past }), q, false},
		{"platform web", mod(func(p *Placement) { p.Platform = "web" }), q, false},
		{"locale other", mod(func(p *Placement) { p.Locale = "en-US" }), q, false},
		{"locale same", mod(func(p *Placement) { p.Locale = "zh-cn" }), q, true},
		{"region hit", mod(func(p *Placement) { p.Regions = []string{"SG", "HK"} }), q, true},
		{"region miss", mod(func(p *Placement) { p.Regions = []string{"HK"} }), q, false},
		{"new anon", mod(func(p *Placement) { p.Audience = "new" }), q, true},
		{"new vs existing", mod(func(p *Placement) { p.Audience = "new" }), placementQuery{Platform: "app", AppVersion: "1.4.0", Region: "SG", Audience: "existing", Now: now}, false},
		{"existing anon", mod(func(p *Placement) { p.Audience = "existing" }), q, false},
		{"ver in", mod(func(p *Placement) { p.AppMinVersion, p.AppMaxVersion = "1.3", "1.10" }), q, true},
		{"ver below", mod(func(p *Placement) { p.AppMinVersion = "1.10.0" }), q, false},
		{"ver above", mod(func(p *Placement) { p.AppMaxVersion = "1.3.9" }), q, false},
		{"ver missing", mod(func(p *Placement) { p.AppMinVersion = "1.0" }), placementQuery{Platform: "app", Region: "SG", Now: now}, false},
		{"ver ignored on web", mod(func(p *Placement) { p.AppMinVersion = "9.0" }), placementQuery{Platform: "web", Region: "SG", Now: now}, true},
	}
	for _, c := range cases {
		if got := placementVisible(c.p, c.q); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestPlacementValidate(t *testing.T) {
	ok := func() placementInput {
		return placementInput{Slot: "carousel", Title: "t", ImageURL: "https://cdn.x/a.png", LinkURL: "/earn", Regions: []string{"sg"}}
	}
	in := ok()
	if msg := in.validate(); msg != "" || in.Platform != "all" || in.Frequency != "always" || in.Regions[0] != "SG" || string(in.Content) != "{}" {
		t.Fatalf("valid rejected: %q %+v", msg, in)
	}
	bad := []func(*placementInput){
		func(p *placementInput) { p.Slot = "footer" },
		func(p *placementInput) { p.Title = "" },
		func(p *placementInput) { p.ImageURL = "" }, // required for carousel
		func(p *placementInput) { p.LinkURL = "javascript:alert(1)" },
		func(p *placementInput) { p.ImageDarkURL = "//evil" },
		func(p *placementInput) { p.Platform = "ios" },
		func(p *placementInput) { p.Audience = "vip" },
		func(p *placementInput) { p.Frequency = "hourly" },
		func(p *placementInput) { p.AppMinVersion = "v1" },
		func(p *placementInput) { p.AppMinVersion, p.AppMaxVersion = "2.0", "1.9" },
		func(p *placementInput) { p.Regions = []string{"SGP"} },
		func(p *placementInput) { p.Content = []byte(`[1]`) },
		func(p *placementInput) { s := time.Now(); p.StartAt, p.EndAt = &s, &s },
	}
	for i, f := range bad {
		in := ok()
		f(&in)
		if in.validate() == "" {
			t.Errorf("case %d accepted", i)
		}
	}
	hint := placementInput{Slot: "search_hint", Title: "BTC", Content: []byte(`{"keyword":"BTC"}`)}
	if msg := hint.validate(); msg != "" {
		t.Fatalf("search_hint without image rejected: %s", msg)
	}
	if cmpVersion("1.10", "1.9") <= 0 || cmpVersion("1.2", "1.2.0") != 0 {
		t.Fatal("cmpVersion")
	}
}
