package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// 全站地理封锁。名单来自 market-data tradfi_settings.restricted_countries（默认美国、属地和 OFAC）。
// 国家只认 CF-IPCountry，且请求须已被 withClientIdentity 信任：TCP 对端在 TRUSTED_PROXY_CIDRS，
// 或 X-Linbit-Edge-Auth 等于可用的 GATEWAY_EDGE_SECRET。
// GEO_BLOCK_ENABLED=true/1/yes/on 开，false/0/no/off 关；未设置时主网默认开。
// /healthz 在此检查之前返回。国家未知放行。名单加载失败也放行（见 siteGeoCheck）。
// TradFi 的 check() 对未知国家是拒绝，不要和这里混用。
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
	// 未信任、缺头、XX、T1 都是空：全站封锁放行。
	if c == "" {
		return true
	}
	g, err := t.snapshot(r.Context())
	if err != nil || g == nil {
		// 受限名单读不到：放行。这里不是失败关闭。
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
