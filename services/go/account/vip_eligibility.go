package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/shopspring/decimal"

	"github.com/linbit/platform/services/go/common"
)

// vip_eligibility.go: server-side VIP eligibility, enforced when an application
// is submitted (a hard block; the reviewer still decides afterwards).
//
// Eligible = total assets >= VIP_MIN_TOTAL_ASSETS_USDT OR 30-day trading volume
// >= VIP_MIN_VOLUME_30D_USDT (defaults 500,000 / 50,000,000 USDT, the published
// VIPConditions).
//   - Assets: asset service GET /internal/valuation — funding + spot + futures
//     wallets valued at the market-data index price.
//   - Volume: growth service GET /internal/growth/volume — spot (USD-quoted
//     pairs) + futures notional, USDT, trailing 30 days.
//
// It fails closed: when a source is down, or an asset could not be priced and
// the figures that are known do not already clear a threshold, the application
// is refused with a retryable 503 (VIP_ELIGIBILITY_UNAVAILABLE) — never
// approved on missing data. A value that is only a lower bound (unpriced asset)
// is still trusted when it already clears the threshold.

const (
	defaultVIPMinAssets    = "500000"
	defaultVIPMinVolume30d = "50000000"
	vipVolumeDays          = 30
	vipEligibilityTimeout  = 4 * time.Second
)

// errVIPDataUnavailable: assets or volume could not be determined (retryable).
var errVIPDataUnavailable = errors.New("vip eligibility data unavailable")

// vipThresholds returns the configured minimums, falling back to the published
// defaults for empty / invalid / non-positive values.
func (s *Server) vipThresholds() (minAssets, minVolume decimal.Decimal) {
	parse := func(v, def string) decimal.Decimal {
		if d, err := decimal.NewFromString(v); err == nil && d.IsPositive() {
			return d
		}
		return decimal.RequireFromString(def)
	}
	return parse(s.cfg.VIPMinAssetsUSDT, defaultVIPMinAssets), parse(s.cfg.VIPMinVolume30dUSDT, defaultVIPMinVolume30d)
}

// vipConditions is the published eligibility block of GET /vip/application; it
// always reflects the thresholds actually enforced.
func (s *Server) vipConditions() map[string]string {
	a, v := s.vipThresholds()
	return map[string]string{"minTotalAssetsUSDT": a.String(), "min30dVolumeUSDT": v.String(), "reviewDays": VIPConditions["reviewDays"]}
}

// vipCheck is the outcome of an eligibility evaluation.
type vipCheck struct {
	Eligible bool
	Assets   decimal.Decimal // USDT; a lower bound when the valuation was incomplete
	Volume   decimal.Decimal // USDT, trailing 30 days
}

// checkVIPEligibility evaluates the applicant. A non-nil error is always
// errVIPDataUnavailable (fail closed).
func (s *Server) checkVIPEligibility(ctx context.Context, uid string) (vipCheck, error) {
	minAssets, minVolume := s.vipThresholds()
	ctx, cancel := context.WithTimeout(ctx, 2*vipEligibilityTimeout)
	defer cancel()

	var res vipCheck
	assets, complete, aerr := s.vipAssets(ctx, uid)
	if aerr == nil {
		res.Assets = assets
		if assets.GreaterThanOrEqual(minAssets) {
			res.Eligible = true
			return res, nil
		}
	}
	vol, verr := s.vipVolume(ctx, uid)
	if verr == nil {
		res.Volume = vol
		if vol.GreaterThanOrEqual(minVolume) {
			res.Eligible = true
			return res, nil
		}
	}
	if aerr != nil || !complete || verr != nil {
		return res, errVIPDataUnavailable
	}
	return res, nil
}

func (s *Server) vipAssets(ctx context.Context, uid string) (total decimal.Decimal, complete bool, err error) {
	cctx, cancel := context.WithTimeout(ctx, vipEligibilityTimeout)
	defer cancel()
	code, body, err := s.internalCall(cctx, http.MethodGet, s.cfg.AssetInternalURL, "/internal/valuation?userId="+url.QueryEscape(uid), nil, nil)
	if err != nil {
		return total, false, err
	}
	if code != http.StatusOK {
		return total, false, fmt.Errorf("asset valuation status %d", code)
	}
	var out struct {
		TotalValueUsdt string `json:"totalValueUsdt"`
		Complete       *bool  `json:"complete"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Complete == nil {
		return total, false, fmt.Errorf("asset valuation: bad response")
	}
	total, err = decimal.NewFromString(out.TotalValueUsdt)
	if err != nil || total.IsNegative() {
		return decimal.Zero, false, fmt.Errorf("asset valuation: bad total")
	}
	return total, *out.Complete, nil
}

func (s *Server) vipVolume(ctx context.Context, uid string) (decimal.Decimal, error) {
	cctx, cancel := context.WithTimeout(ctx, vipEligibilityTimeout)
	defer cancel()
	path := fmt.Sprintf("/internal/growth/volume?uid=%s&days=%d", url.QueryEscape(uid), vipVolumeDays)
	code, body, err := s.internalCall(cctx, http.MethodGet, s.cfg.GrowthInternalURL, path, nil, nil)
	if err != nil {
		return decimal.Zero, err
	}
	if code != http.StatusOK {
		return decimal.Zero, fmt.Errorf("growth volume status %d", code)
	}
	var out struct {
		TotalUsdt string `json:"totalUsdt"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.TotalUsdt == "" {
		return decimal.Zero, fmt.Errorf("growth volume: bad response")
	}
	v, err := decimal.NewFromString(out.TotalUsdt)
	if err != nil || v.IsNegative() {
		return decimal.Zero, fmt.Errorf("growth volume: bad total")
	}
	return v, nil
}

// writeVIPNotEligible answers 403 VIP_NOT_ELIGIBLE with the applicant's own
// figures. Same APIError envelope as common.WriteError plus an `eligibility` object.
func (s *Server) writeVIPNotEligible(w http.ResponseWriter, c vipCheck) {
	minAssets, minVolume := s.vipThresholds()
	msg := fmt.Sprintf("暂不满足 VIP 申请条件：需账户总资产 ≥ %s USDT，或近 30 日交易量 ≥ %s USDT（您当前总资产约 %s USDT，近 30 日交易量约 %s USDT）",
		fmtUSDT(minAssets), fmtUSDT(minVolume), fmtUSDT(c.Assets), fmtUSDT(c.Volume))
	common.WriteJSON(w, http.StatusForbidden, map[string]any{
		"code": "VIP_NOT_ELIGIBLE", "message": msg, "requestId": common.RequestID(),
		"eligibility": map[string]string{
			"minTotalAssetsUSDT": minAssets.String(), "min30dVolumeUSDT": minVolume.String(),
			"totalAssetsUSDT": c.Assets.StringFixed(2), "volume30dUSDT": c.Volume.StringFixed(2),
		},
	})
}

// writeVIPUnavailable answers 503 VIP_ELIGIBILITY_UNAVAILABLE (retryable).
func writeVIPUnavailable(w http.ResponseWriter) {
	common.WriteError(w, http.StatusServiceUnavailable, "VIP_ELIGIBILITY_UNAVAILABLE", "暂时无法核验您的资产与交易量，请稍后重试")
}

// fmtUSDT renders a USDT amount with thousands separators and no trailing zeros
// beyond the cent (e.g. 500000 -> "500,000", 1234.5 -> "1,234.5").
func fmtUSDT(d decimal.Decimal) string {
	s := d.Truncate(2).String()
	neg := false
	if len(s) > 0 && s[0] == '-' {
		neg, s = true, s[1:]
	}
	intPart, frac := s, ""
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			intPart, frac = s[:i], s[i:]
			break
		}
	}
	var out []byte
	for i := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, intPart[i])
	}
	res := string(out) + frac
	if neg {
		res = "-" + res
	}
	return res
}
