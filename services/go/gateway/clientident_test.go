package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testEdgeSecret = "0123456789abcdef0123456789abcdef"

type seen struct {
	ip, xff, cf, country, edge string
	trusted                    bool
}

func runIdent(t *testing.T, peer string, h map[string]string) seen {
	t.Helper()
	trusted, _ := parseCIDRs("172.18.0.1/32")
	var s seen
	hd := withClientIdentity(trusted, testEdgeSecret, "X-Real-IP", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s = seen{r.Header.Get("X-Real-IP"), r.Header.Get("X-Forwarded-For"), r.Header.Get("CF-Connecting-IP"), trustedCountry(r, nil), r.Header.Get(edgeAuthHeader), false}
		s.trusted, _ = requestTrusted(r)
	}))
	req := httptest.NewRequest("GET", "/market/ticker", nil)
	req.RemoteAddr = peer
	for k, v := range h {
		req.Header.Set(k, v)
	}
	hd.ServeHTTP(httptest.NewRecorder(), req)
	return s
}

func TestClientIdentity_TrustedPeer(t *testing.T) {
	s := runIdent(t, "172.18.0.1:5555", map[string]string{"X-Real-IP": "203.0.113.7", "X-Forwarded-For": "1.1.1.1, 2.2.2.2", "CF-IPCountry": "us"})
	if s.ip != "203.0.113.7" || s.xff != "203.0.113.7" || s.cf != "203.0.113.7" || s.country != "US" || !s.trusted {
		t.Fatalf("%+v", s)
	}
}

func TestClientIdentity_EdgeSecret(t *testing.T) {
	s := runIdent(t, "172.18.0.9:5555", map[string]string{edgeAuthHeader: testEdgeSecret, "X-Real-IP": "198.51.100.4", "CF-IPCountry": "IR"})
	if s.ip != "198.51.100.4" || s.country != "IR" || !s.trusted || s.edge != "" {
		t.Fatalf("%+v", s)
	}
}

func TestClientIdentity_SpoofRejected(t *testing.T) {
	for _, h := range []map[string]string{
		{"X-Real-IP": "8.8.8.8", "X-Forwarded-For": "8.8.8.8", "CF-Connecting-IP": "8.8.8.8", "CF-IPCountry": "SG"},
		{edgeAuthHeader: "wrong-secret-wrong-secret-wrong-secret", "X-Real-IP": "8.8.8.8", "CF-IPCountry": "SG"},
		{edgeAuthHeader: testEdgeSecret[:31], "X-Real-IP": "8.8.8.8"},
	} {
		s := runIdent(t, "172.18.0.9:5555", h)
		if s.ip != "172.18.0.9" || s.xff != "172.18.0.9" || s.country != "" || s.trusted || s.edge != "" {
			t.Fatalf("spoof accepted: %v -> %+v", h, s)
		}
	}
}

func TestClientIdentity_InvalidHeaderFallsBackToPeer(t *testing.T) {
	s := runIdent(t, "172.18.0.1:1", map[string]string{"X-Real-IP": "not-an-ip<script>"})
	if s.ip != "172.18.0.1" {
		t.Fatalf("%+v", s)
	}
}

func TestClientIdentity_ShortSecretIgnored(t *testing.T) {
	trusted, _ := parseCIDRs("172.18.0.1/32")
	var ok bool
	h := withClientIdentity(trusted, "short", "X-Real-IP", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ok, _ = requestTrusted(r) }))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.2:1"
	req.Header.Set(edgeAuthHeader, "short")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if ok {
		t.Fatal("short secret must not grant trust")
	}
}

// End to end through Handler(): a spoofed X-Forwarded-For from an untrusted
// peer never reaches upstream; the upstream sees the peer IP only.
func TestHandlerStripsSpoofedIdentity(t *testing.T) {
	var gotXFF, gotEdge string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXFF, gotEdge = r.Header.Get("X-Forwarded-For"), r.Header.Get(edgeAuthHeader)
		w.Write([]byte("{}"))
	}))
	defer up.Close()
	gw, err := NewGateway(Config{JWTSecret: "s", AccountURL: up.URL, AssetURL: up.URL, MarketURL: up.URL, TrustedProxyCIDRs: "10.9.9.9/32", EdgeSecret: testEdgeSecret, ClientIPHeader: "X-Real-IP"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/market/ticker", nil) // peer 192.0.2.1, untrusted
	req.Header.Set("X-Forwarded-For", "6.6.6.6")
	req.Header.Set(edgeAuthHeader, "nope")
	gw.Handler().ServeHTTP(httptest.NewRecorder(), req)
	if gotXFF != "192.0.2.1, 192.0.2.1" && gotXFF != "192.0.2.1" || gotEdge != "" {
		t.Fatalf("upstream XFF=%q edge=%q", gotXFF, gotEdge)
	}
	req = httptest.NewRequest("GET", "/market/ticker", nil)
	req.Header.Set("X-Real-IP", "198.51.100.4")
	req.Header.Set(edgeAuthHeader, testEdgeSecret)
	gw.Handler().ServeHTTP(httptest.NewRecorder(), req)
	if !strings.HasPrefix(gotXFF, "198.51.100.4") || gotEdge != "" {
		t.Fatalf("edge-auth upstream XFF=%q edge=%q", gotXFF, gotEdge)
	}
}
