package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// App feature switches.
//
//	public   GET /app/features            {features:{key:bool}, updatedAt}   (cached 30s)
//	internal GET /internal/app/features   same + overrides
//	internal PUT /internal/app/features   {features:{key:bool}, updatedBy} -> {features, before, after}
//
// Known keys and defaults: only convert is shipped; everything else is off
// until built. Unknown keys are rejected so a typo can't create a dead flag.
var featureDefaults = map[string]bool{
	"convert":         true,
	"tokenizedStocks": false,
	"metals":          false,
	"eventContracts":  false,
	"chat":            false,
	"subAccounts":     false,
	"copyTrading":     false,
	"p2p":             false,
	"fiatOnRamp":      false,
	"options":         false,
}

var featCache struct {
	mu  sync.Mutex
	at  time.Time
	val map[string]any
}

func (s *Store) featureOverrides(ctx context.Context) (map[string]bool, time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, enabled, updated_at FROM app_features`)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()
	out := map[string]bool{}
	var last time.Time
	for rows.Next() {
		var k string
		var v bool
		var at time.Time
		if err := rows.Scan(&k, &v, &at); err != nil {
			return nil, last, err
		}
		out[k] = v
		if at.After(last) {
			last = at
		}
	}
	return out, last, rows.Err()
}

// mergeFeatures applies stored overrides (known keys only) to the defaults.
func mergeFeatures(over map[string]bool) map[string]bool {
	f := map[string]bool{}
	for k, v := range featureDefaults {
		f[k] = v
	}
	for k, v := range over {
		if _, ok := featureDefaults[k]; ok {
			f[k] = v
		}
	}
	return f
}

func (s *Server) features(ctx context.Context) (map[string]any, error) {
	over, last, err := s.store.featureOverrides(ctx)
	if err != nil {
		return nil, err
	}
	var updated any
	if !last.IsZero() {
		updated = last.UTC()
	}
	return map[string]any{"features": mergeFeatures(over), "updatedAt": updated}, nil
}

func (s *Server) handleAppFeatures(w http.ResponseWriter, r *http.Request) {
	featCache.mu.Lock()
	defer featCache.mu.Unlock()
	if featCache.val == nil || time.Since(featCache.at) >= 30*time.Second {
		v, err := s.features(r.Context())
		if err != nil {
			if featCache.val == nil {
				// DB down and nothing cached: serve defaults rather than break app start-up.
				v = map[string]any{"features": mergeFeatures(nil), "updatedAt": nil}
			} else {
				v = featCache.val
			}
		}
		featCache.val, featCache.at = v, time.Now()
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	common.WriteJSON(w, http.StatusOK, featCache.val)
}

func (s *Server) hIntFeatures(w http.ResponseWriter, r *http.Request) {
	v, err := s.features(r.Context())
	if err != nil {
		intErr(w, "features", err)
		return
	}
	keys := make([]string, 0, len(featureDefaults))
	for k := range featureDefaults {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	v["keys"], v["defaults"] = keys, featureDefaults
	common.WriteJSON(w, http.StatusOK, v)
}

// PUT: partial update — only the keys sent change.
func (s *Server) hIntFeaturesPut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Features  map[string]bool `json:"features"`
		UpdatedBy string          `json:"updatedBy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.Features) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "features 必填")
		return
	}
	for k := range in.Features {
		if _, ok := featureDefaults[k]; !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "未知功能开关: "+k)
			return
		}
	}
	over, _, err := s.store.featureOverrides(r.Context())
	if err != nil {
		intErr(w, "features", err)
		return
	}
	before := mergeFeatures(over)
	tx, err := s.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		intErr(w, "features tx", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range in.Features {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO app_features (key, enabled, updated_by) VALUES ($1,$2,$3)
			ON CONFLICT (key) DO UPDATE SET enabled=$2, updated_by=$3, updated_at=now()`, k, v, in.UpdatedBy); err != nil {
			intErr(w, "features put", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		intErr(w, "features commit", err)
		return
	}
	featCache.mu.Lock()
	featCache.val = nil
	featCache.mu.Unlock()
	v, err := s.features(r.Context())
	if err != nil {
		intErr(w, "features", err)
		return
	}
	v["before"], v["after"] = before, v["features"]
	common.WriteJSON(w, http.StatusOK, v)
}

func (s *Server) featureRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /app/features", s.handleAppFeatures)
	mux.HandleFunc("GET /internal/app/features", s.internal(s.hIntFeatures))
	mux.HandleFunc("PUT /internal/app/features", s.internal(s.hIntFeaturesPut))
}
