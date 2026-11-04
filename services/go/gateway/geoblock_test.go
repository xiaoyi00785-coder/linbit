package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSiteGeoBlock(t *testing.T) {
	tg := newTradFiGuard(Config{TrustedProxyCIDRs: "10.0.0.0/8"})
	tg.siteGeo = true
	tg.gate, tg.gateAt = &tradfiGate{RestrictedCountries: []string{"US", "IR"}}, time.Now()
	for _, c := range []struct {
		peer, cc string
		want     bool
	}{{"10.1.1.1:1", "US", false}, {"10.1.1.1:1", "JP", true}, {"8.8.8.8:1", "US", true}, {"10.1.1.1:1", "", true}} {
		r := httptest.NewRequest(http.MethodGet, "/markets", nil)
		r.RemoteAddr = c.peer
		if c.cc != "" {
			r.Header.Set("CF-IPCountry", c.cc)
		}
		w := httptest.NewRecorder()
		if got := tg.siteGeoCheck(w, r); got != c.want {
			t.Fatalf("%+v: got %v", c, got)
		}
		if !c.want && w.Code != http.StatusUnavailableForLegalReasons {
			t.Fatalf("code %d", w.Code)
		}
	}
}
