package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/linbit/platform/services/go/common"
)

// Academy content. Public reads return only published articles. Nothing is seeded.

var academyIDRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,31})$`)

type academyCategory struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Sort    int    `json:"sort"`
	Enabled bool   `json:"enabled,omitempty"`
}

type academyArticle struct {
	ID          int64  `json:"id,string"`
	CategoryID  string `json:"categoryId"`
	Category    string `json:"category,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Body        string `json:"body,omitempty"`
	Status      string `json:"status,omitempty"`
	Format      string `json:"format"`
	PublishedAt string `json:"publishedAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

func (s *Server) academyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /academy/categories", s.handleAcademyCategories)
	mux.HandleFunc("GET /academy/articles", s.handleAcademyArticles)
	mux.HandleFunc("GET /academy/articles/{id}", s.handleAcademyArticle)
	mux.HandleFunc("GET /market/search", s.handleMarketSearch)
	mux.HandleFunc("GET /internal/academy/categories", s.internal(s.hIntAcademyCategories))
	mux.HandleFunc("POST /internal/academy/categories", s.internal(s.hIntAcademySaveCategory))
	mux.HandleFunc("PUT /internal/academy/categories/{id}", s.internal(s.hIntAcademySaveCategory))
	mux.HandleFunc("DELETE /internal/academy/categories/{id}", s.internal(s.hIntAcademyDeleteCategory))
	mux.HandleFunc("GET /internal/academy/articles", s.internal(s.hIntAcademyArticles))
	mux.HandleFunc("POST /internal/academy/articles", s.internal(s.hIntAcademySaveArticle))
	mux.HandleFunc("PUT /internal/academy/articles/{id}", s.internal(s.hIntAcademySaveArticle))
	mux.HandleFunc("POST /internal/academy/articles/{id}/publish", s.internal(s.hIntAcademySetStatus("published")))
	mux.HandleFunc("POST /internal/academy/articles/{id}/unpublish", s.internal(s.hIntAcademySetStatus("draft")))
	s.academyEngagementRoutes(mux)
}

func (s *Server) academyDB() *sql.DB {
	if s.store == nil {
		return nil
	}
	return s.store.db
}

func (s *Server) handleAcademyCategories(w http.ResponseWriter, r *http.Request) {
	items := []academyCategory{}
	db := s.academyDB()
	if db == nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	rows, err := db.QueryContext(r.Context(), `SELECT id, name, sort FROM academy_categories WHERE enabled ORDER BY sort, id`)
	if err != nil {
		log.Printf("academy categories: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var c academyCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.Sort); err != nil {
			log.Printf("academy categories scan: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		items = append(items, c)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAcademyArticles(w http.ResponseWriter, r *http.Request) {
	items := []academyArticle{}
	db := s.academyDB()
	if db == nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": false})
		return
	}
	q := r.URL.Query()
	limit := atoiDefault(q.Get("limit"), 20, 1, 50)
	before := atoiDefault(q.Get("before"), 0, 0, 1<<62)
	cat := strings.TrimSpace(q.Get("category"))
	locale := strings.TrimSpace(q.Get("locale"))
	args := []any{}
	where := `WHERE a.status='published' AND c.enabled`
	if cat != "" {
		args = append(args, cat)
		where += ` AND a.category_id=$` + strconv.Itoa(len(args))
	}
	if locale != "" {
		args = append(args, locale)
		where += ` AND (a.locale='' OR a.locale=$` + strconv.Itoa(len(args)) + `)`
	}
	if before > 0 {
		args = append(args, before)
		where += ` AND a.id<$` + strconv.Itoa(len(args))
	}
	args = append(args, limit+1)
	rows, err := db.QueryContext(r.Context(), `SELECT a.id, a.category_id, a.locale, a.title, a.summary, COALESCE(to_char(a.published_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		FROM academy_articles a JOIN academy_categories c ON c.id=a.category_id `+where+
		` ORDER BY a.id DESC LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		log.Printf("academy articles: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a academyArticle
		if err := rows.Scan(&a.ID, &a.CategoryID, &a.Locale, &a.Title, &a.Summary, &a.PublishedAt); err != nil {
			log.Printf("academy articles scan: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		a.Category, a.Format = a.CategoryID, "plain"
		items = append(items, a)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore})
}

func (s *Server) handleAcademyArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	db := s.academyDB()
	if db == nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "文章不存在")
		return
	}
	var a academyArticle
	err = db.QueryRowContext(r.Context(), `SELECT a.id, a.category_id, a.locale, a.title, a.summary, a.body, COALESCE(to_char(a.published_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		FROM academy_articles a JOIN academy_categories c ON c.id=a.category_id
		WHERE a.id=$1 AND a.status='published' AND c.enabled`, id).Scan(&a.ID, &a.CategoryID, &a.Locale, &a.Title, &a.Summary, &a.Body, &a.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "文章不存在")
		return
	}
	if err != nil {
		log.Printf("academy article: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	a.Category, a.Format = a.CategoryID, "plain"
	common.WriteJSON(w, http.StatusOK, a)
}

func (s *Server) handleMarketSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || utf8.RuneCountInString(q) > 64 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "q 必填且不超过 64 字")
		return
	}
	symbols := s.searchSymbols(r.Context(), strings.ToUpper(q))
	articles := []academyArticle{}
	if db := s.academyDB(); db != nil {
		var err error
		articles, err = s.searchArticles(r.Context(), q)
		if err != nil {
			log.Printf("market search articles: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"query": q, "symbols": symbols, "articles": articles})
}

func (s *Server) searchSymbols(ctx context.Context, q string) []symbolDTO {
	out := []symbolDTO{}
	if s.store != nil && s.store.db != nil {
		infos, err := s.store.Symbols(ctx)
		if err == nil {
			for _, si := range infos {
				if si.Status == "delisted" {
					continue
				}
				if strings.Contains(si.Symbol, q) || strings.Contains(strings.ToUpper(si.Base), q) || strings.Contains(strings.ToUpper(si.Quote), q) {
					out = append(out, symbolDTO{Symbol: si.Symbol, Base: si.Base, Quote: si.Quote, PricePrecision: si.PricePrecision, QuantityPrecision: si.QuantityPrecision, Status: si.Status, Tags: []string{}})
				}
				if len(out) >= 20 {
					break
				}
			}
			return out
		}
	}
	if s.agg == nil {
		return out
	}
	for _, sym := range s.agg.TrackedSymbols() {
		if strings.Contains(strings.ToUpper(sym), q) {
			out = append(out, symbolDTO{Symbol: sym, Status: "trading", Tags: []string{}})
		}
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func (s *Server) searchArticles(ctx context.Context, q string) ([]academyArticle, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT a.id, a.category_id, a.locale, a.title, a.summary, COALESCE(to_char(a.published_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		FROM academy_articles a JOIN academy_categories c ON c.id=a.category_id
		WHERE a.status='published' AND c.enabled AND (a.title ILIKE $1 ESCAPE '\\' OR a.summary ILIKE $1 ESCAPE '\\')
		ORDER BY a.id DESC LIMIT 20`, "%"+escapeLike(q)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []academyArticle{}
	for rows.Next() {
		var a academyArticle
		if err := rows.Scan(&a.ID, &a.CategoryID, &a.Locale, &a.Title, &a.Summary, &a.PublishedAt); err != nil {
			return nil, err
		}
		a.Category, a.Format = a.CategoryID, "plain"
		out = append(out, a)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func validAcademyText(s string, max int, required bool) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	if required && n == 0 {
		return false
	}
	return n <= max
}

func (s *Server) hIntAcademyCategories(w http.ResponseWriter, r *http.Request) {
	db := s.academyDB()
	if db == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "数据库未配置")
		return
	}
	rows, err := db.QueryContext(r.Context(), `SELECT id, name, sort, enabled FROM academy_categories ORDER BY sort, id`)
	if err != nil {
		log.Printf("academy admin categories: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	items := []academyCategory{}
	for rows.Next() {
		var c academyCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.Sort, &c.Enabled); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		items = append(items, c)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) hIntAcademySaveCategory(w http.ResponseWriter, r *http.Request) {
	db := s.academyDB()
	if db == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "数据库未配置")
		return
	}
	var in struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Sort      int    `json:"sort"`
		Enabled   *bool  `json:"enabled"`
		UpdatedBy string `json:"updatedBy"`
	}
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	id := strings.ToLower(strings.TrimSpace(in.ID))
	if r.PathValue("id") != "" {
		id = strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	}
	if !academyIDRe.MatchString(id) || !validAcademyText(in.Name, 40, true) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "分类 id 或名称不合法")
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	_, err := db.ExecContext(r.Context(), `INSERT INTO academy_categories (id, name, sort, enabled, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,now())
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, sort=EXCLUDED.sort, enabled=EXCLUDED.enabled, updated_by=EXCLUDED.updated_by, updated_at=now()`,
		id, strings.TrimSpace(in.Name), in.Sort, enabled, strings.TrimSpace(in.UpdatedBy))
	if err != nil {
		log.Printf("academy save category: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) hIntAcademyDeleteCategory(w http.ResponseWriter, r *http.Request) {
	db := s.academyDB()
	if db == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "数据库未配置")
		return
	}
	id := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	var n int
	if err := db.QueryRowContext(r.Context(), `SELECT count(*) FROM academy_articles WHERE category_id=$1`, id).Scan(&n); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if n > 0 {
		common.WriteError(w, http.StatusConflict, "CATEGORY_NOT_EMPTY", "分类下仍有文章")
		return
	}
	res, err := db.ExecContext(r.Context(), `DELETE FROM academy_categories WHERE id=$1`, id)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "分类不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) hIntAcademyArticles(w http.ResponseWriter, r *http.Request) {
	db := s.academyDB()
	if db == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "数据库未配置")
		return
	}
	st := r.URL.Query().Get("status")
	q := `SELECT id, category_id, locale, title, summary, status FROM academy_articles`
	args := []any{}
	if st == "draft" || st == "published" {
		q += ` WHERE status=$1`
		args = append(args, st)
	}
	q += ` ORDER BY id DESC LIMIT 100`
	rows, err := db.QueryContext(r.Context(), q, args...)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	items := []academyArticle{}
	for rows.Next() {
		var a academyArticle
		if err := rows.Scan(&a.ID, &a.CategoryID, &a.Locale, &a.Title, &a.Summary, &a.Status); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		a.Format = "plain"
		items = append(items, a)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) hIntAcademySaveArticle(w http.ResponseWriter, r *http.Request) {
	db := s.academyDB()
	if db == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "数据库未配置")
		return
	}
	var in struct {
		CategoryID string `json:"categoryId"`
		Locale     string `json:"locale"`
		Title      string `json:"title"`
		Summary    string `json:"summary"`
		Body       string `json:"body"`
		UpdatedBy  string `json:"updatedBy"`
	}
	if err := common.DecodeJSON(r, &in); err != nil || !validAcademyText(in.Title, 120, true) || !validAcademyText(in.Summary, 300, false) || !validAcademyText(in.Body, 50000, false) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "标题、摘要或正文不合法")
		return
	}
	cat := strings.ToLower(strings.TrimSpace(in.CategoryID))
	if !academyIDRe.MatchString(cat) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "分类不合法")
		return
	}
	var exists int
	if err := db.QueryRowContext(r.Context(), `SELECT count(*) FROM academy_categories WHERE id=$1`, cat).Scan(&exists); err != nil || exists == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "分类不存在")
		return
	}
	if id := r.PathValue("id"); id != "" {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
			return
		}
		res, err := db.ExecContext(r.Context(), `UPDATE academy_articles SET category_id=$2, locale=$3, title=$4, summary=$5, body=$6, updated_by=$7, updated_at=now() WHERE id=$1`,
			n, cat, strings.TrimSpace(in.Locale), strings.TrimSpace(in.Title), strings.TrimSpace(in.Summary), in.Body, strings.TrimSpace(in.UpdatedBy))
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if aff, _ := res.RowsAffected(); aff == 0 {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "文章不存在")
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"id": n, "status": "unchanged"})
		return
	}
	var id int64
	err := db.QueryRowContext(r.Context(), `INSERT INTO academy_articles (category_id, locale, title, summary, body, status, updated_by)
		VALUES ($1,$2,$3,$4,$5,'draft',$6) RETURNING id`,
		cat, strings.TrimSpace(in.Locale), strings.TrimSpace(in.Title), strings.TrimSpace(in.Summary), in.Body, strings.TrimSpace(in.UpdatedBy)).Scan(&id)
	if err != nil {
		log.Printf("academy insert: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "draft"})
}

func (s *Server) hIntAcademySetStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := s.academyDB()
		if db == nil {
			common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "数据库未配置")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
			return
		}
		var req struct {
			UpdatedBy string `json:"updatedBy"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
				return
			}
		}
		res, err := db.ExecContext(r.Context(), `UPDATE academy_articles SET status=$2, updated_by=$3, updated_at=now(), published_at=CASE WHEN $2='published' THEN now() ELSE NULL END WHERE id=$1`, id, status, strings.TrimSpace(req.UpdatedBy))
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if aff, _ := res.RowsAffected(); aff == 0 {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "文章不存在")
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": status})
	}
}
