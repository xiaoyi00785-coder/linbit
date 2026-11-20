package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/linbit/platform/services/go/common"
)

// Academy engagement: related articles, popular lists, likes, bookmarks and
// comments. All read endpoints are safe for anonymous callers; writes require
// a logged-in uid (gateway X-User-Id).

func (s *Server) academyEngagementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /academy/articles/popular", s.handleAcademyPopular)
	mux.HandleFunc("GET /academy/articles/{id}/related", s.handleAcademyRelated)
	mux.HandleFunc("POST /academy/articles/{id}/view", s.handleAcademyView)
	mux.HandleFunc("POST /academy/articles/{id}/like", s.handleAcademyLike(true))
	mux.HandleFunc("DELETE /academy/articles/{id}/like", s.handleAcademyLike(false))
	mux.HandleFunc("POST /academy/articles/{id}/bookmark", s.handleAcademyBookmark(true))
	mux.HandleFunc("DELETE /academy/articles/{id}/bookmark", s.handleAcademyBookmark(false))
	mux.HandleFunc("GET /academy/bookmarks", s.handleAcademyBookmarks)
	mux.HandleFunc("GET /academy/articles/{id}/comments", s.handleAcademyComments)
	mux.HandleFunc("POST /academy/articles/{id}/comments", s.handleAcademyPostComment)
	mux.HandleFunc("DELETE /academy/comments/{id}", s.handleAcademyDeleteComment)
}

func (s *Server) handleAcademyPopular(w http.ResponseWriter, r *http.Request) {
	items := []academyArticle{}
	db := s.academyDB()
	if db == nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	limit := atoiDefault(r.URL.Query().Get("limit"), 10, 1, 50)
	rows, err := db.QueryContext(r.Context(), `SELECT a.id, a.category_id, a.locale, a.title, a.summary,
		COALESCE(to_char(a.published_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''), a.views, a.likes
		FROM academy_articles a JOIN academy_categories c ON c.id=a.category_id
		WHERE a.status='published' AND c.enabled ORDER BY a.views DESC, a.id DESC LIMIT $1`, limit)
	if err != nil {
		log.Printf("academy popular: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	type popItem struct {
		academyArticle
		Views int64 `json:"views"`
		Likes int64 `json:"likes"`
	}
	popular := []popItem{}
	for rows.Next() {
		var a popItem
		if err := rows.Scan(&a.ID, &a.CategoryID, &a.Locale, &a.Title, &a.Summary, &a.PublishedAt, &a.Views, &a.Likes); err != nil {
			log.Printf("academy popular scan: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		a.Category, a.Format = a.CategoryID, "plain"
		popular = append(popular, a)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": popular})
}

func (s *Server) handleAcademyRelated(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	db := s.academyDB()
	items := []academyArticle{}
	if db == nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	limit := atoiDefault(r.URL.Query().Get("limit"), 5, 1, 20)
	// Same category, exclude self, prefer newer.
	rows, err := db.QueryContext(r.Context(), `SELECT a.id, a.category_id, a.locale, a.title, a.summary,
		COALESCE(to_char(a.published_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		FROM academy_articles a JOIN academy_categories c ON c.id=a.category_id
		WHERE a.status='published' AND c.enabled AND a.id != $1
		  AND a.category_id = (SELECT category_id FROM academy_articles WHERE id=$1)
		ORDER BY a.id DESC LIMIT $2`, id, limit)
	if err != nil {
		log.Printf("academy related: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a academyArticle
		if err := rows.Scan(&a.ID, &a.CategoryID, &a.Locale, &a.Title, &a.Summary, &a.PublishedAt); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		a.Category, a.Format = a.CategoryID, "plain"
		items = append(items, a)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAcademyView(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	db := s.academyDB()
	if db == nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE academy_articles SET views=views+1 WHERE id=$1 AND status='published'`, id)
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAcademyLike(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := strings.TrimSpace(r.Header.Get("X-User-Id"))
		if uid == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
			return
		}
		db := s.academyDB()
		if db == nil {
			common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "暂不可用")
			return
		}
		if add {
			res, err := db.ExecContext(r.Context(), `INSERT INTO academy_likes (article_id, uid) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, uid)
			if err != nil {
				common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
				return
			}
			if n, _ := res.RowsAffected(); n == 1 {
				_, _ = db.ExecContext(r.Context(), `UPDATE academy_articles SET likes=likes+1 WHERE id=$1`, id)
			}
		} else {
			res, err := db.ExecContext(r.Context(), `DELETE FROM academy_likes WHERE article_id=$1 AND uid=$2`, id, uid)
			if err != nil {
				common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
				return
			}
			if n, _ := res.RowsAffected(); n == 1 {
				_, _ = db.ExecContext(r.Context(), `UPDATE academy_articles SET likes=GREATEST(likes-1, 0) WHERE id=$1`, id)
			}
		}
		var likes int64
		_ = db.QueryRowContext(r.Context(), `SELECT likes FROM academy_articles WHERE id=$1`, id).Scan(&likes)
		var exists int
		_ = db.QueryRowContext(r.Context(), `SELECT 1 FROM academy_likes WHERE article_id=$1 AND uid=$2`, id, uid).Scan(&exists)
		common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "likes": likes, "liked": exists == 1})
	}
}

func (s *Server) handleAcademyBookmark(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := strings.TrimSpace(r.Header.Get("X-User-Id"))
		if uid == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
			return
		}
		db := s.academyDB()
		if db == nil {
			common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "暂不可用")
			return
		}
		if add {
			_, err = db.ExecContext(r.Context(), `INSERT INTO academy_bookmarks (article_id, uid) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, uid)
		} else {
			_, err = db.ExecContext(r.Context(), `DELETE FROM academy_bookmarks WHERE article_id=$1 AND uid=$2`, id, uid)
		}
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "bookmarked": add})
	}
}

func (s *Server) handleAcademyBookmarks(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	db := s.academyDB()
	if db == nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	rows, err := db.QueryContext(r.Context(), `SELECT a.id, a.category_id, a.locale, a.title, a.summary,
		COALESCE(to_char(a.published_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''), b.at
		FROM academy_bookmarks b JOIN academy_articles a ON a.id=b.article_id
		JOIN academy_categories c ON c.id=a.category_id
		WHERE b.uid=$1 AND a.status='published' AND c.enabled ORDER BY b.at DESC LIMIT 200`, uid)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	type item struct {
		academyArticle
		BookmarkedAt string `json:"bookmarkedAt"`
	}
	items := []item{}
	for rows.Next() {
		var a item
		var at string
		if err := rows.Scan(&a.ID, &a.CategoryID, &a.Locale, &a.Title, &a.Summary, &a.PublishedAt, &at); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		a.Category, a.Format, a.BookmarkedAt = a.CategoryID, "plain", at
		items = append(items, a)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Comments.

func (s *Server) handleAcademyComments(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	db := s.academyDB()
	items := []map[string]any{}
	if db == nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	limit := atoiDefault(r.URL.Query().Get("limit"), 20, 1, 50)
	before := atoiDefault(r.URL.Query().Get("before"), 0, 0, 1<<62)
	q := `SELECT id, uid, body, at FROM academy_comments WHERE article_id=$1 AND status='visible'`
	args := []any{id}
	if before > 0 {
		q += ` AND id < $2`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ` + strconv.Itoa(limit+1)
	rows, err := db.QueryContext(r.Context(), q, args...)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var cid int64
		var uid, body string
		var at sql.NullTime
		if err := rows.Scan(&cid, &uid, &body, &at); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		item := map[string]any{"id": cid, "uid": maskUIDAcademy(uid), "body": body}
		if at.Valid {
			item["at"] = at.Time.UnixMilli()
		}
		items = append(items, item)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore})
}

func (s *Server) handleAcademyPostComment(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	db := s.academyDB()
	if db == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "暂不可用")
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
		return
	}
	body.Body = strings.TrimSpace(body.Body)
	if body.Body == "" || utf8.RuneCountInString(body.Body) > 500 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "评论不可为空，最多 500 字")
		return
	}
	var cid int64
	err = db.QueryRowContext(r.Context(), `INSERT INTO academy_comments (article_id, uid, body) VALUES ($1,$2,$3) RETURNING id`, id, uid, body.Body).Scan(&cid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "文章不存在")
			return
		}
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": cid, "articleId": id, "uid": maskUIDAcademy(uid), "body": body.Body})
}

func (s *Server) handleAcademyDeleteComment(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	db := s.academyDB()
	if db == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "暂不可用")
		return
	}
	res, err := db.ExecContext(r.Context(), `DELETE FROM academy_comments WHERE id=$1 AND uid=$2`, id, uid)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "评论不存在或无权删除")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// maskUIDAcademy keeps the first 3 and last 3 chars, masks the middle.
func maskUIDAcademy(uid string) string {
	if len(uid) <= 7 {
		return uid
	}
	return uid[:3] + "****" + uid[len(uid)-3:]
}
