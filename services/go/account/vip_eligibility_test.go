package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// vipUpstreams fakes the asset valuation and growth volume endpoints.
type vipUpstreams struct {
	assetBody, growthBody     string
	assetStatus, growthStatus int
	growthCalls               atomic.Int32
	gotAssetQuery, gotGrowth  atomic.Value
}

func newVIPServer(t *testing.T, up *vipUpstreams) *Server {
	t.Helper()
	if up.assetStatus == 0 {
		up.assetStatus = 200
	}
	if up.growthStatus == 0 {
		up.growthStatus = 200
	}
	asset := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/valuation" || r.Header.Get("X-Internal-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		up.gotAssetQuery.Store(r.URL.RawQuery)
		w.WriteHeader(up.assetStatus)
		w.Write([]byte(up.assetBody))
	}))
	growth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/growth/volume" || r.Header.Get("X-Internal-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		up.growthCalls.Add(1)
		up.gotGrowth.Store(r.URL.RawQuery)
		w.WriteHeader(up.growthStatus)
		w.Write([]byte(up.growthBody))
	}))
	t.Cleanup(asset.Close)
	t.Cleanup(growth.Close)
	return &Server{cfg: Config{InternalToken: "tok", AssetInternalURL: asset.URL, GrowthInternalURL: growth.URL}, http: &http.Client{Timeout: 2 * time.Second}}
}

func assetsJSON(total string, complete bool) string {
	b, _ := json.Marshal(map[string]any{"totalValueUsdt": total, "complete": complete})
	return string(b)
}
func volumeJSON(total string) string {
	b, _ := json.Marshal(map[string]any{"totalUsdt": total})
	return string(b)
}

func TestVIPEligibilityMatrix(t *testing.T) {
	for _, tc := range []struct {
		name         string
		up           *vipUpstreams
		wantEligible bool
		wantErr      bool
		wantGrowth   int32 // growth calls expected
	}{
		{name: "assets at threshold (inclusive), volume not consulted", up: &vipUpstreams{assetBody: assetsJSON("500000.00", true)}, wantEligible: true},
		{name: "volume at threshold, assets low", up: &vipUpstreams{assetBody: assetsJSON("10.00", true), growthBody: volumeJSON("50000000")}, wantEligible: true, wantGrowth: 1},
		{name: "both below -> not eligible", up: &vipUpstreams{assetBody: assetsJSON("499999.99", true), growthBody: volumeJSON("49999999.99")}, wantGrowth: 1},
		{name: "incomplete valuation but lower bound already clears", up: &vipUpstreams{assetBody: assetsJSON("600000.00", false)}, wantEligible: true},
		{name: "incomplete valuation below threshold, volume low -> unavailable", up: &vipUpstreams{assetBody: assetsJSON("100.00", false), growthBody: volumeJSON("0")}, wantErr: true, wantGrowth: 1},
		{name: "incomplete valuation below threshold, volume clears -> eligible", up: &vipUpstreams{assetBody: assetsJSON("100.00", false), growthBody: volumeJSON("60000000")}, wantEligible: true, wantGrowth: 1},
		{name: "asset service down, volume low -> unavailable", up: &vipUpstreams{assetStatus: 500, assetBody: `{}`, growthBody: volumeJSON("1")}, wantErr: true, wantGrowth: 1},
		{name: "asset service down, volume clears -> eligible", up: &vipUpstreams{assetStatus: 503, assetBody: `{}`, growthBody: volumeJSON("50000001")}, wantEligible: true, wantGrowth: 1},
		{name: "growth down, assets low -> unavailable", up: &vipUpstreams{assetBody: assetsJSON("1.00", true), growthStatus: 502, growthBody: `{}`}, wantErr: true, wantGrowth: 1},
		{name: "growth down, assets clear -> eligible", up: &vipUpstreams{assetBody: assetsJSON("700000.00", true), growthStatus: 502, growthBody: `{}`}, wantEligible: true},
		{name: "malformed asset body (no complete flag) never counts", up: &vipUpstreams{assetBody: `{"totalValueUsdt":"900000.00"}`, growthBody: volumeJSON("0")}, wantErr: true, wantGrowth: 1},
		{name: "garbage asset body", up: &vipUpstreams{assetBody: `nope`, growthBody: volumeJSON("0")}, wantErr: true, wantGrowth: 1},
		{name: "negative total rejected", up: &vipUpstreams{assetBody: assetsJSON("-5", true), growthBody: volumeJSON("0")}, wantErr: true, wantGrowth: 1},
		{name: "missing volume field", up: &vipUpstreams{assetBody: assetsJSON("1", true), growthBody: `{}`}, wantErr: true, wantGrowth: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := tc.up
			s := newVIPServer(t, up)
			chk, err := s.checkVIPEligibility(context.Background(), "U 1")
			if tc.wantErr {
				if !errors.Is(err, errVIPDataUnavailable) || chk.Eligible {
					t.Fatalf("want fail-closed unavailable, got %+v err=%v", chk, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if chk.Eligible != tc.wantEligible {
				t.Fatalf("eligible=%v want %v (%+v)", chk.Eligible, tc.wantEligible, chk)
			}
			if got := up.growthCalls.Load(); got != tc.wantGrowth {
				t.Fatalf("growth calls=%d want %d", got, tc.wantGrowth)
			}
		})
	}
}

func TestVIPEligibilityQueryAndFigures(t *testing.T) {
	up := &vipUpstreams{assetBody: assetsJSON("1234.50", true), growthBody: volumeJSON("99.25")}
	s := newVIPServer(t, up)
	chk, err := s.checkVIPEligibility(context.Background(), "U 1")
	if err != nil || chk.Eligible {
		t.Fatalf("%+v %v", chk, err)
	}
	if !chk.Assets.Equal(decimal.RequireFromString("1234.5")) || !chk.Volume.Equal(decimal.RequireFromString("99.25")) {
		t.Fatalf("figures %+v", chk)
	}
	if q, _ := up.gotAssetQuery.Load().(string); q != "userId=U+1" {
		t.Fatalf("asset query %q", q)
	}
	if q, _ := up.gotGrowth.Load().(string); q != "uid=U+1&days=30" {
		t.Fatalf("growth query %q", q)
	}
}

func TestVIPEligibilityUpstreamsUnconfigured(t *testing.T) {
	s := &Server{cfg: Config{InternalToken: "tok"}, http: &http.Client{Timeout: time.Second}}
	if chk, err := s.checkVIPEligibility(context.Background(), "U1"); !errors.Is(err, errVIPDataUnavailable) || chk.Eligible {
		t.Fatalf("unconfigured upstreams must fail closed: %+v %v", chk, err)
	}
}

func TestVIPThresholdsConfigurable(t *testing.T) {
	up := &vipUpstreams{assetBody: assetsJSON("1000.00", true), growthBody: volumeJSON("0")}
	s := newVIPServer(t, up)
	s.cfg.VIPMinAssetsUSDT = "1000"
	s.cfg.VIPMinVolume30dUSDT = "5"
	if chk, err := s.checkVIPEligibility(context.Background(), "U1"); err != nil || !chk.Eligible {
		t.Fatalf("lowered threshold: %+v %v", chk, err)
	}
	// published defaults apply when unset / invalid / non-positive
	for _, bad := range []string{"", "abc", "0", "-1"} {
		s.cfg.VIPMinAssetsUSDT, s.cfg.VIPMinVolume30dUSDT = bad, bad
		a, v := s.vipThresholds()
		if a.String() != "500000" || v.String() != "50000000" {
			t.Fatalf("%q -> %s %s", bad, a, v)
		}
	}
	s.cfg.VIPMinAssetsUSDT, s.cfg.VIPMinVolume30dUSDT = "250000", "10000000"
	c := s.vipConditions()
	if c["minTotalAssetsUSDT"] != "250000" || c["min30dVolumeUSDT"] != "10000000" || c["reviewDays"] != "1-3" {
		t.Fatalf("published conditions must mirror enforced thresholds: %v", c)
	}
}

func TestVIPErrorResponses(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.writeVIPNotEligible(rec, vipCheck{Assets: decimal.RequireFromString("1234.5"), Volume: decimal.RequireFromString("0")})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Code, Message, RequestID string
		Eligibility              map[string]string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "VIP_NOT_ELIGIBLE" || body.RequestID == "" || body.Eligibility["minTotalAssetsUSDT"] != "500000" || body.Eligibility["totalAssetsUSDT"] != "1234.50" {
		t.Fatalf("%+v", body)
	}
	for _, want := range []string{"500,000", "50,000,000", "1,234.5", "暂不满足 VIP 申请条件"} {
		if !strings.Contains(body.Message, want) {
			t.Fatalf("message %q lacks %q", body.Message, want)
		}
	}
	rec = httptest.NewRecorder()
	writeVIPUnavailable(rec)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "VIP_ELIGIBILITY_UNAVAILABLE") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestFmtUSDT(t *testing.T) {
	for in, want := range map[string]string{"500000": "500,000", "50000000": "50,000,000", "0": "0", "999": "999", "1234.5": "1,234.5", "1234567.891": "1,234,567.89", "-1234": "-1,234"} {
		if got := fmtUSDT(decimal.RequireFromString(in)); got != want {
			t.Fatalf("%s -> %s want %s", in, got, want)
		}
	}
}

// POST /vip/applications end to end (needs DATABASE_URL, like the other
// handler tests): ineligible -> 403, upstream down -> 503 (nothing stored),
// existing application answered without consulting the upstreams, eligible ->
// pending.
func TestApplyVIPEligibilityGate(t *testing.T) {
	srv := newTestServer(t)
	up := &vipUpstreams{assetBody: assetsJSON("10.00", true), growthBody: volumeJSON("0")}
	fake := newVIPServer(t, up)
	srv.cfg.InternalToken, srv.cfg.AssetInternalURL, srv.cfg.GrowthInternalURL = fake.cfg.InternalToken, fake.cfg.AssetInternalURL, fake.cfg.GrowthInternalURL
	h := srv.Routes()
	tok := registerWithCode(t, srv, uniqueEmail(), "", "").AccessToken
	apply := func() *httptest.ResponseRecorder {
		return doJSON(t, h, "POST", "/vip/applications", tok, map[string]string{"channel": "telegram", "contact": "@vip_gate_test"})
	}
	errCode := func(w *httptest.ResponseRecorder) string {
		var b struct{ Code string }
		_ = json.Unmarshal(w.Body.Bytes(), &b)
		return b.Code
	}

	if w := apply(); w.Code != http.StatusForbidden || errCode(w) != "VIP_NOT_ELIGIBLE" {
		t.Fatalf("ineligible: %d %s", w.Code, w.Body.String())
	}
	down := newVIPServer(t, &vipUpstreams{assetStatus: 503, assetBody: `{}`, growthStatus: 503, growthBody: `{}`})
	srv.cfg.AssetInternalURL, srv.cfg.GrowthInternalURL = down.cfg.AssetInternalURL, down.cfg.GrowthInternalURL
	if w := apply(); w.Code != http.StatusServiceUnavailable || errCode(w) != "VIP_ELIGIBILITY_UNAVAILABLE" {
		t.Fatalf("upstream down must fail closed: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, h, "GET", "/vip/application", tok, nil); !strings.Contains(w.Body.String(), `"status":"none"`) {
		t.Fatalf("a refused application must not be stored: %s", w.Body.String())
	}
	ok := newVIPServer(t, &vipUpstreams{assetBody: assetsJSON("900000.00", true)})
	srv.cfg.AssetInternalURL, srv.cfg.GrowthInternalURL = ok.cfg.AssetInternalURL, ok.cfg.GrowthInternalURL
	if w := apply(); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"pending"`) {
		t.Fatalf("eligible: %d %s", w.Code, w.Body.String())
	}
	// pending answers even when the upstreams are down
	srv.cfg.AssetInternalURL, srv.cfg.GrowthInternalURL = down.cfg.AssetInternalURL, down.cfg.GrowthInternalURL
	if w := apply(); w.Code != http.StatusConflict || errCode(w) != "APPLICATION_PENDING" {
		t.Fatalf("pending: %d %s", w.Code, w.Body.String())
	}
	// the published conditions reflect the enforced thresholds
	if w := doJSON(t, h, "GET", "/vip/application", tok, nil); !strings.Contains(w.Body.String(), `"minTotalAssetsUSDT":"500000"`) {
		t.Fatalf("conditions: %s", w.Body.String())
	}
}
