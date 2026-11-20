package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

func (s *Server) itemRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /nft/items", s.handleItems)
	mux.HandleFunc("GET /nft/items/{id}", s.optUser(s.handleItemDetail))
	mux.HandleFunc("POST /nft/items/{id}/favorite", s.user(s.handleFavoriteToggle))
	mux.HandleFunc("GET /nft/favorites", s.user(s.handleFavorites))
	mux.HandleFunc("GET /nft/my/items", s.user(s.handleMyItems))
	mux.HandleFunc("GET /nft/my/activity", s.user(s.handleMyActivity))
}

func (s *Server) handleItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where, args := []string{"i.status='active'"}, []any{}
	if raw := q.Get("collection"); raw != "" {
		cid, err := s.resolveCollectionID(r.Context(), raw)
		if err != nil {
			writeErr(w, err)
			return
		}
		args = append(args, cid)
		where = append(where, fmt.Sprintf("i.collection_id=$%d", len(args)))
	}
	if owner := q.Get("owner"); owner != "" {
		args = append(args, owner)
		where = append(where, fmt.Sprintf("i.owner_id=$%d", len(args)))
	}
	if q.Get("listed") == "true" {
		where = append(where, "i.listed")
	}
	sortBy := "i.id DESC"
	switch q.Get("sort") {
	case "price_asc":
		sortBy = "i.list_price NULLS LAST, i.id"
	case "price_desc":
		sortBy = "i.list_price DESC NULLS LAST, i.id DESC"
	case "recent":
		sortBy = "i.id DESC"
	case "favorite":
		sortBy = "i.favorite_count DESC, i.id DESC"
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT `+prefixed("i", itemCols)+` FROM nft_items i WHERE `+strings.Join(where, " AND ")+
			` ORDER BY `+sortBy+` LIMIT `+strconv.Itoa(limit)+` OFFSET `+strconv.Itoa(offset), args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "nextOffset": offset + len(items)})
}

func (s *Server) resolveCollectionID(ctx context.Context, raw string) (int64, error) {
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return id, nil
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM nft_collections WHERE slug=$1`, raw).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, notFound("COLLECTION_NOT_FOUND", "合集不存在")
	}
	return id, err
}

// handleItemDetail returns the item plus its collection, the active listing (if
// any), the top three live offers and whether the caller has favourited it.
func (s *Server) handleItemDetail(w http.ResponseWriter, r *http.Request, viewer string) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	it, err := s.loadItem(r.Context(), s.db, id, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	col, err := s.loadCollection(r.Context(), s.db, it.CollectionID, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := map[string]any{"item": it, "collection": col}
	if l := s.activeListing(r.Context(), it.ID); l != nil {
		out["listing"] = l
	}
	offers, err := s.topOffers(r.Context(), it.ID, 5)
	if err != nil {
		writeErr(w, err)
		return
	}
	out["offers"] = offers
	if viewer != "" {
		var fav bool
		_ = s.db.QueryRowContext(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM nft_favorites WHERE uid=$1 AND item_id=$2)`, viewer, it.ID).Scan(&fav)
		out["favorited"] = fav
	}
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handleFavoriteToggle(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	it, err := s.loadItem(ctx, tx, id, true)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM nft_favorites WHERE uid=$1 AND item_id=$2`, uid, it.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	n, _ := res.RowsAffected()
	favorited := n == 0
	if favorited {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO nft_favorites (uid, item_id, created_at) VALUES ($1,$2,$3)`, uid, it.ID, s.now()); err != nil {
			writeErr(w, err)
			return
		}
		if _, err := tx.ExecContext(ctx, `UPDATE nft_items SET favorite_count=favorite_count+1 WHERE id=$1`, it.ID); err != nil {
			writeErr(w, err)
			return
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE nft_items SET favorite_count=GREATEST(favorite_count-1,0) WHERE id=$1`, it.ID); err != nil {
			writeErr(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"itemId": it.ID, "favorited": favorited})
}

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request, uid string) {
	limit, offset := pageParams(r)
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT `+prefixed("i", itemCols)+` FROM nft_favorites f
		 JOIN nft_items i ON i.id=f.item_id
		 WHERE f.uid=$1 ORDER BY f.created_at DESC LIMIT $2 OFFSET $3`, uid, limit, offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "nextOffset": offset + len(items)})
}

func (s *Server) handleMyItems(w http.ResponseWriter, r *http.Request, uid string) {
	limit, offset := pageParams(r)
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT `+prefixed("i", itemCols)+` FROM nft_items i
		 WHERE i.owner_id=$1 AND i.status='active' ORDER BY i.listed DESC, i.id DESC LIMIT $2 OFFSET $3`,
		uid, limit, offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "nextOffset": offset + len(items)})
}

func (s *Server) handleMyActivity(w http.ResponseWriter, r *http.Request, uid string) {
	limit, _ := pageParams(r)
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, item_id, collection_id, kind, from_uid, to_uid, price, asset, detail, at
		 FROM nft_activity WHERE from_uid=$1 OR to_uid=$1
		 ORDER BY id DESC LIMIT $2`, uid, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": scanActivityRows(rows)})
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, bad("VALIDATION_ERROR", "id 不合法")
	}
	return id, nil
}

func pageParams(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func prefixed(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}
