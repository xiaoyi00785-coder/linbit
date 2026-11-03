package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Home placements: one model for every operations slot on the home page.
// "Banners" are slot=carousel; GET /home/banners stays as a compatible alias.

var placementSlots = []string{"popup", "splash", "grid", "carousel", "promo_bar", "search_hint"}

func isSlot(s string) bool {
	for _, x := range placementSlots {
		if x == s {
			return true
		}
	}
	return false
}

type Placement struct {
	ID             int64           `json:"id"`
	Slot           string          `json:"slot"`
	Title          string          `json:"title"`
	ImageURL       string          `json:"imageUrl"`
	ImageDarkURL   string          `json:"imageDarkUrl"`
	LinkURL        string          `json:"linkUrl"`
	Platform       string          `json:"platform"`
	Locale         string          `json:"locale"`
	Audience       string          `json:"audience"`
	Regions        []string        `json:"regions"`
	AppMinVersion  string          `json:"appMinVersion"`
	AppMaxVersion  string          `json:"appMaxVersion"`
	Frequency      string          `json:"frequency"`
	CountdownEndAt *time.Time      `json:"countdownEndAt"`
	TrackingID     string          `json:"trackingId"`
	EarnProductID  string          `json:"earnProductId"`
	EarnAPR        *string         `json:"earnApr,omitempty"` // public only, live from earn
	Content        json.RawMessage `json:"content"`
	Sort           int             `json:"sort"`
	StartAt        *time.Time      `json:"startAt"`
	EndAt          *time.Time      `json:"endAt"`
	Enabled        bool            `json:"enabled"`
	UpdatedBy      string          `json:"updatedBy,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type placementInput struct {
	Slot           string          `json:"slot"`
	Title          string          `json:"title"`
	ImageURL       string          `json:"imageUrl"`
	ImageDarkURL   string          `json:"imageDarkUrl"`
	LinkURL        string          `json:"linkUrl"`
	Platform       string          `json:"platform"`
	Locale         string          `json:"locale"`
	Audience       string          `json:"audience"`
	Regions        []string        `json:"regions"`
	AppMinVersion  string          `json:"appMinVersion"`
	AppMaxVersion  string          `json:"appMaxVersion"`
	Frequency      string          `json:"frequency"`
	CountdownEndAt *time.Time      `json:"countdownEndAt"`
	TrackingID     string          `json:"trackingId"`
	EarnProductID  string          `json:"earnProductId"`
	Content        json.RawMessage `json:"content"`
	Sort           int             `json:"sort"`
	StartAt        *time.Time      `json:"startAt"`
	EndAt          *time.Time      `json:"endAt"`
	Enabled        *bool           `json:"enabled"`
	UpdatedBy      string          `json:"updatedBy"`
}

var (
	reVersion  = regexp.MustCompile(`^\d+(\.\d+){0,3}$`)
	reRegion   = regexp.MustCompile(`^[A-Z]{2}$`)
	reTracking = regexp.MustCompile(`^[A-Za-z0-9_\-:.]{0,64}$`)
)

// imageRequired: slots whose client rendering needs an image (banners,
// splash, popup, grid tiles). Rows in these slots without imageUrl are
// rejected on write and hidden on public read.
func imageRequired(slot string) bool {
	return slot == "carousel" || slot == "splash" || slot == "popup" || slot == "grid"
}

func okURL(u string, required bool) bool {
	if u == "" {
		return !required
	}
	if strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") {
		return true // in-app route
	}
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	switch p.Scheme {
	case "https", "http":
		return p.Host != ""
	case "linbit", "cowa": // app deep links
		return true
	}
	return false
}

func (in *placementInput) validate() string {
	in.Slot = strings.TrimSpace(in.Slot)
	in.Title = strings.TrimSpace(in.Title)
	in.Platform = strings.ToLower(strings.TrimSpace(in.Platform))
	in.Audience = strings.ToLower(strings.TrimSpace(in.Audience))
	in.Frequency = strings.ToLower(strings.TrimSpace(in.Frequency))
	if in.Platform == "" {
		in.Platform = "all"
	}
	if in.Audience == "" {
		in.Audience = "all"
	}
	if in.Frequency == "" {
		in.Frequency = "always"
	}
	switch {
	case !isSlot(in.Slot):
		return "slot 必须为 popup/splash/grid/carousel/promo_bar/search_hint"
	case in.Title == "" || len([]rune(in.Title)) > 120:
		return "title 必填且不超过 120 字"
	case !okURL(in.ImageURL, imageRequired(in.Slot)):
		return "imageUrl 不合法（该位置必填）"
	case !okURL(in.ImageDarkURL, false):
		return "imageDarkUrl 不合法"
	case !okURL(in.LinkURL, false):
		return "linkUrl 不合法"
	case in.Platform != "web" && in.Platform != "app" && in.Platform != "all":
		return "platform 必须为 web/app/all"
	case in.Audience != "all" && in.Audience != "new" && in.Audience != "existing":
		return "audience 必须为 all/new/existing"
	case in.Frequency != "always" && in.Frequency != "once" && in.Frequency != "daily" && in.Frequency != "session":
		return "frequency 必须为 always/once/daily/session"
	case in.AppMinVersion != "" && !reVersion.MatchString(in.AppMinVersion),
		in.AppMaxVersion != "" && !reVersion.MatchString(in.AppMaxVersion):
		return "appMinVersion/appMaxVersion 格式应为 1.2.3"
	case in.AppMinVersion != "" && in.AppMaxVersion != "" && cmpVersion(in.AppMinVersion, in.AppMaxVersion) > 0:
		return "appMinVersion 不能大于 appMaxVersion"
	case !reTracking.MatchString(in.TrackingID):
		return "trackingId 不合法"
	case len(in.EarnProductID) > 64:
		return "earnProductId 过长"
	case in.StartAt != nil && in.EndAt != nil && !in.EndAt.After(*in.StartAt):
		return "endAt 必须晚于 startAt"
	}
	for i, r := range in.Regions {
		r = strings.ToUpper(strings.TrimSpace(r))
		if !reRegion.MatchString(r) {
			return "regions 必须为两位国家/地区代码"
		}
		in.Regions[i] = r
	}
	if len(in.Content) == 0 || string(in.Content) == "null" {
		in.Content = json.RawMessage(`{}`)
	}
	var obj map[string]any
	if json.Unmarshal(in.Content, &obj) != nil || len(in.Content) > 4096 {
		return "content 必须为不超过 4KB 的 JSON 对象"
	}
	return ""
}

// cmpVersion compares dotted numeric versions ("1.10" > "1.9").
func cmpVersion(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// placementQuery is what the client tells us about itself.
type placementQuery struct {
	Platform, Locale, AppVersion, Region, Audience string
	Now                                            time.Time
}

// placementVisible decides whether a placement is shown to this client.
func placementVisible(p Placement, q placementQuery) bool {
	if !p.Enabled || (p.StartAt != nil && q.Now.Before(*p.StartAt)) || (p.EndAt != nil && !q.Now.Before(*p.EndAt)) {
		return false
	}
	if p.Platform != "all" && q.Platform != "" && p.Platform != q.Platform {
		return false
	}
	if p.Locale != "" && q.Locale != "" && !strings.EqualFold(p.Locale, q.Locale) {
		return false
	}
	// Audience: unknown audience (anonymous, client didn't say) only sees "all"
	// and "new" (a visitor without an account is a prospective new user).
	switch p.Audience {
	case "new":
		if q.Audience == "existing" {
			return false
		}
	case "existing":
		if q.Audience != "existing" {
			return false
		}
	}
	if len(p.Regions) > 0 {
		hit := false
		for _, r := range p.Regions {
			if r == q.Region {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	// App version range only constrains app clients; an app client that
	// doesn't send appVersion is excluded from version-gated placements.
	if q.Platform == "app" && (p.AppMinVersion != "" || p.AppMaxVersion != "") {
		if q.AppVersion == "" {
			return false
		}
		if p.AppMinVersion != "" && cmpVersion(q.AppVersion, p.AppMinVersion) < 0 {
			return false
		}
		if p.AppMaxVersion != "" && cmpVersion(q.AppVersion, p.AppMaxVersion) > 0 {
			return false
		}
	}
	return true
}

// ---- store ----

const placementCols = `id, slot, title, image_url, image_dark_url, link_url, platform, locale, audience,
	array_to_string(regions, ','), app_min_version, app_max_version, frequency, countdown_end_at, tracking_id,
	earn_product_id, content::text, sort, start_at, end_at, enabled, updated_by, created_at, updated_at`

func scanPlacement(sc interface{ Scan(...any) error }) (Placement, error) {
	var p Placement
	var regions, content string
	var cd, st, en sql.NullTime
	err := sc.Scan(&p.ID, &p.Slot, &p.Title, &p.ImageURL, &p.ImageDarkURL, &p.LinkURL, &p.Platform, &p.Locale, &p.Audience,
		&regions, &p.AppMinVersion, &p.AppMaxVersion, &p.Frequency, &cd, &p.TrackingID,
		&p.EarnProductID, &content, &p.Sort, &st, &en, &p.Enabled, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.Regions = splitCSV(regions)
	if p.Regions == nil {
		p.Regions = []string{}
	}
	p.Content = json.RawMessage(content)
	for _, x := range []struct {
		n *sql.NullTime
		d **time.Time
	}{{&cd, &p.CountdownEndAt}, {&st, &p.StartAt}, {&en, &p.EndAt}} {
		if x.n.Valid {
			t := x.n.Time.UTC()
			*x.d = &t
		}
	}
	return p, nil
}

// Placements lists placements, optionally of one slot.
func (s *Store) Placements(ctx context.Context, slot string) ([]Placement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+placementCols+` FROM home_placements WHERE ($1='' OR slot=$1) ORDER BY slot, sort, id`, slot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Placement{}
	for rows.Next() {
		p, err := scanPlacement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Placement(ctx context.Context, id int64) (Placement, error) {
	return scanPlacement(s.db.QueryRowContext(ctx, `SELECT `+placementCols+` FROM home_placements WHERE id=$1`, id))
}

func (s *Store) SavePlacement(ctx context.Context, id int64, in placementInput) (Placement, error) {
	enabled := in.Enabled == nil || *in.Enabled
	args := []any{in.Slot, in.Title, in.ImageURL, in.ImageDarkURL, in.LinkURL, in.Platform, in.Locale, in.Audience,
		strings.Join(in.Regions, ","), in.AppMinVersion, in.AppMaxVersion, in.Frequency, in.CountdownEndAt, in.TrackingID,
		in.EarnProductID, string(in.Content), in.Sort, in.StartAt, in.EndAt, enabled, in.UpdatedBy}
	var row *sql.Row
	if id == 0 {
		row = s.db.QueryRowContext(ctx, `INSERT INTO home_placements (slot, title, image_url, image_dark_url, link_url, platform, locale, audience,
			regions, app_min_version, app_max_version, frequency, countdown_end_at, tracking_id, earn_product_id, content, sort, start_at, end_at, enabled, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8, COALESCE(string_to_array(NULLIF($9,''), ','), '{}'),$10,$11,$12,$13,$14,$15,$16::jsonb,$17,$18,$19,$20,$21)
			RETURNING `+placementCols, args...)
	} else {
		row = s.db.QueryRowContext(ctx, `UPDATE home_placements SET slot=$1, title=$2, image_url=$3, image_dark_url=$4, link_url=$5, platform=$6,
			locale=$7, audience=$8, regions=COALESCE(string_to_array(NULLIF($9,''), ','), '{}'), app_min_version=$10, app_max_version=$11,
			frequency=$12, countdown_end_at=$13, tracking_id=$14, earn_product_id=$15, content=$16::jsonb, sort=$17, start_at=$18,
			end_at=$19, enabled=$20, updated_by=$21, updated_at=now() WHERE id=$22 RETURNING `+placementCols, append(args, id)...)
	}
	return scanPlacement(row)
}

func (s *Store) DeletePlacement(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM home_placements WHERE id=$1`, id)
	return err
}

// ---- public ----

type placementCache struct {
	mu   sync.Mutex
	at   time.Time
	all  []Placement
	apr  map[string]string
	aprA time.Time
}

var plCache placementCache

func (s *Server) cachedPlacements(ctx context.Context) ([]Placement, error) {
	plCache.mu.Lock()
	defer plCache.mu.Unlock()
	if plCache.all != nil && time.Since(plCache.at) < 15*time.Second {
		return plCache.all, nil
	}
	all, err := s.store.Placements(ctx, "")
	if err != nil {
		return nil, err
	}
	plCache.all, plCache.at = all, time.Now()
	return all, nil
}

func invalidatePlacements() {
	plCache.mu.Lock()
	plCache.all = nil
	plCache.mu.Unlock()
}

// earnAPRs returns product id -> aprMax from the earn service (public
// GET /earn/products), cached 60s. On failure the stale map (or nil) is used,
// so a placement just omits earnApr rather than showing a made-up number.
func (s *Server) earnAPRs(ctx context.Context) map[string]string {
	plCache.mu.Lock()
	defer plCache.mu.Unlock()
	if plCache.apr != nil && time.Since(plCache.aprA) < 60*time.Second {
		return plCache.apr
	}
	if s.cfg.EarnURL == "" {
		return plCache.apr
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(c, "GET", strings.TrimRight(s.cfg.EarnURL, "/")+"/earn/products", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return plCache.apr
	}
	defer resp.Body.Close()
	var body struct {
		Items []struct {
			ID     json.RawMessage `json:"id"`
			APRMax json.RawMessage `json:"aprMax"`
		} `json:"items"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&body) != nil {
		return plCache.apr
	}
	m := map[string]string{}
	for _, it := range body.Items {
		m[strings.Trim(string(it.ID), `"`)] = strings.Trim(string(it.APRMax), `"`)
	}
	plCache.apr, plCache.aprA = m, time.Now()
	return m
}

func queryFrom(r *http.Request) placementQuery {
	q := r.URL.Query()
	region := strings.ToUpper(strings.TrimSpace(q.Get("region")))
	if region == "" {
		region = strings.ToUpper(r.Header.Get("CF-IPCountry"))
	}
	return placementQuery{
		Platform:   strings.ToLower(q.Get("platform")),
		Locale:     q.Get("locale"),
		AppVersion: q.Get("appVersion"),
		Region:     region,
		Audience:   strings.ToLower(q.Get("audience")),
		Now:        time.Now(),
	}
}

func (s *Server) visiblePlacements(r *http.Request, slots []string) (map[string][]Placement, error) {
	all, err := s.cachedPlacements(r.Context())
	if err != nil {
		return nil, err
	}
	q := queryFrom(r)
	out := map[string][]Placement{}
	for _, sl := range slots {
		out[sl] = []Placement{}
	}
	var apr map[string]string
	for _, p := range all {
		if _, want := out[p.Slot]; !want || !placementVisible(p, q) {
			continue
		}
		if p.ImageURL == "" && imageRequired(p.Slot) {
			continue // legacy row without an image: never ship a blank banner
		}
		p.UpdatedBy = ""
		if p.EarnProductID != "" {
			if apr == nil {
				apr = s.earnAPRs(r.Context())
			}
			if v, ok := apr[p.EarnProductID]; ok {
				v := v
				p.EarnAPR = &v
			}
		}
		out[p.Slot] = append(out[p.Slot], p)
	}
	for _, l := range out {
		sort.SliceStable(l, func(i, j int) bool { return l[i].Sort < l[j].Sort })
	}
	return out, nil
}

// GET /home/placements?slots=popup,carousel&platform=app&locale=zh-CN&appVersion=1.4.0&region=SG&audience=new
func (s *Server) handleHomePlacements(w http.ResponseWriter, r *http.Request) {
	slots := splitCSV(r.URL.Query().Get("slots"))
	if len(slots) == 0 {
		slots = placementSlots
	}
	for _, sl := range slots {
		if !isSlot(sl) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "未知 slot: "+sl)
			return
		}
	}
	m, err := s.visiblePlacements(r, slots)
	if err != nil {
		intErr(w, "placements", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=15")
	common.WriteJSON(w, http.StatusOK, map[string]any{"slots": m, "serverTime": time.Now().UTC()})
}

// GET /home/banners — compatible alias: active carousel placements.
func (s *Server) handleHomeBanners(w http.ResponseWriter, r *http.Request) {
	m, err := s.visiblePlacements(r, []string{"carousel"})
	if err != nil {
		intErr(w, "banners", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=15")
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": m["carousel"]})
}

// ---- internal (admin proxies here) ----

func (s *Server) placementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /home/placements", s.handleHomePlacements)
	mux.HandleFunc("GET /home/banners", s.handleHomeBanners)
	mux.HandleFunc("GET /internal/home/placements", s.internal(s.hIntPlacementList))
	mux.HandleFunc("POST /internal/home/placements", s.internal(s.hIntPlacementSave))
	mux.HandleFunc("PUT /internal/home/placements/{id}", s.internal(s.hIntPlacementSave))
	mux.HandleFunc("DELETE /internal/home/placements/{id}", s.internal(s.hIntPlacementDelete))
}

func (s *Server) hIntPlacementList(w http.ResponseWriter, r *http.Request) {
	slot := r.URL.Query().Get("slot")
	if slot != "" && !isSlot(slot) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "未知 slot")
		return
	}
	l, err := s.store.Placements(r.Context(), slot)
	if err != nil {
		intErr(w, "placements list", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": l})
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) hIntPlacementSave(w http.ResponseWriter, r *http.Request) {
	var id int64
	var before *Placement
	if r.PathValue("id") != "" {
		var ok bool
		if id, ok = pathID(r); !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
			return
		}
		b, err := s.store.Placement(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "运营位不存在")
			return
		} else if err != nil {
			intErr(w, "placement get", err)
			return
		}
		before = &b
	}
	var in placementInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if msg := in.validate(); msg != "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", msg)
		return
	}
	p, err := s.store.SavePlacement(r.Context(), id, in)
	if err != nil {
		intErr(w, "placement save", err)
		return
	}
	invalidatePlacements()
	st := http.StatusOK
	if id == 0 {
		st = http.StatusCreated
	}
	common.WriteJSON(w, st, map[string]any{"placement": p, "before": before, "after": p})
}

func (s *Server) hIntPlacementDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	b, err := s.store.Placement(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "运营位不存在")
		return
	} else if err != nil {
		intErr(w, "placement get", err)
		return
	}
	if err := s.store.DeletePlacement(r.Context(), id); err != nil {
		intErr(w, "placement delete", err)
		return
	}
	invalidatePlacements()
	common.WriteJSON(w, http.StatusOK, map[string]any{"deleted": id, "before": b, "after": nil})
}
