package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// Site-wide geo block. Uses the admin-configurable restricted-country list
// (market-data tradfi_settings.restricted_countries; default US + territories +
// OFAC set) and the CF-IPCountry header, trusted only from TRUSTED_PROXY_CIDRS.
// Enabled by default on mainnet; GEO_BLOCK_ENABLED=true/false overrides.
// /healthz is answered before this check. An unknown country is allowed here
// (TradFi trading still fails closed in check()).
func geoBlockEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(common.Env("GEO_BLOCK_ENABLED", ""))) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	}
	return common.IsMainnet()
}

const geoBlockedMsg = "您所在的国家或地区暂不支持访问本服务 / Linbit is not available in your country or region"

func (t *tradfiGuard) siteGeoCheck(w http.ResponseWriter, r *http.Request) bool {
	if !t.siteGeo {
		return true
	}
	c := trustedCountry(r, t.trusted)
	if c == "" {
		return true
	}
	g, err := t.snapshot(r.Context())
	if err != nil || g == nil {
		log.Printf("geo block: restricted list unavailable: %v", err)
		return true
	}
	for _, rc := range g.RestrictedCountries {
		if strings.EqualFold(rc, c) {
			common.WriteError(w, http.StatusUnavailableForLegalReasons, "REGION_BLOCKED", geoBlockedMsg)
			return false
		}
	}
	return true
}
