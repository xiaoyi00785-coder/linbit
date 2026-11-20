package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

func (s *Server) collectionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /nft/collections", s.handleCollections)
	mux.HandleFunc("GET /nft/collections/{slug}", s.handleCollectionDetail)
	mux.HandleFunc("GET /nft/collections/top", s.handleTopCollections)
}

func (s *Server) handleCollections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where, args := []string{"status='online'"}, []any{}
	if cat := strings.TrimSpace(q.Get("category")); cat != "" {
		args = append(args, cat)
		where = append(where, fmt.Sprintf("category=$%d", len(args)))
	}
	if kw := strings.TrimSpace(q.Get("q")); kw != "" {
		args = append(args, "%"+strings.ToLower(kw)+"%")
		where = append(where, fmt.Sprintf("(LOWER(name) LIKE $%d OR LOWER(slug) LIKE $%d)", len(args), len(args)))
	}
	sortBy := "volume_24h DESC, id DESC"
	switch q.Get("sort") {
	case "floor":
		sortBy = "floor_price NULLS LAST, id"
	case "items":
		sortBy = "item_count DESC, id"
	case "new":
		sortBy = "created_at DESC, id DESC"
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT `+collectionCols+` FROM nft_collections WHERE `+strings.Join(where, " AND ")+
			` ORDER BY `+sortBy+` LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*Collection{}
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, c)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCollectionDetail(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadCollectionBySlug(r.Context(), s.db, r.PathValue("slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, c)
}

// handleTopCollections ranks collections for the leaderboard: period=24h|7d|all,
// sort=volume|sales|floor.
func (s *Server) handleTopCollections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period := q.Get("period")
	sort := q.Get("sort")
	if sort == "" {
		sort = "volume"
	}
	since := ""
	switch period {
	case "", "24h":
		since = s.now().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	case "7d":
		since = s.now().Add(-7 * 24 * time.Hour).Format(time.RFC3339Nano)
	case "all":
	default:
		writeErr(w, bad("VALIDATION_ERROR", "period 不合法"))
		return
	}
	// Aggregate on the fly so leaderboards stay accurate between ticks.
	var rows interface {
		Next() bool
		Scan(...any) error
		Close() error
	}
	var err error
	if since != "" {
		rows, err = s.db.QueryContext(r.Context(),
			`SELECT c.id, c.slug, c.name, c.image, c.pay_asset, c.floor_price,
			   COALESCE(SUM(t.price),0), COALESCE(COUNT(t.id),0)
			 FROM nft_collections c
			 LEFT JOIN nft_trades t ON t.collection_id=c.id AND t.created_at >= $1
			 WHERE c.status='online' GROUP BY c.id ORDER BY `+rankOrder(sort)+` LIMIT 50`, since)
	} else {
		rows, err = s.db.QueryContext(r.Context(),
			`SELECT c.id, c.slug, c.name, c.image, c.pay_asset, c.floor_price,
			   COALESCE(SUM(t.price),0), COALESCE(COUNT(t.id),0)
			 FROM nft_collections c LEFT JOIN nft_trades t ON t.collection_id=c.id
			 WHERE c.status='online' GROUP BY c.id ORDER BY `+rankOrder(sort)+` LIMIT 50`)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	type ranked struct {
		CollectionID int64            `json:"collectionId"`
		Slug         string           `json:"slug"`
		Name         string           `json:"name"`
		Image        string           `json:"image"`
		Asset        string           `json:"asset"`
		FloorPrice   *decimal.Decimal `json:"floorPrice,omitempty"`
		Volume       decimal.Decimal  `json:"volume"`
		Sales        int64            `json:"sales"`
	}
	items := []ranked{}
	for rows.Next() {
		var r ranked
		var fp decimal.NullDecimal
		if err := rows.Scan(&r.CollectionID, &r.Slug, &r.Name, &r.Image, &r.Asset, &fp, &r.Volume, &r.Sales); err != nil {
			writeErr(w, err)
			return
		}
		if fp.Valid {
			v := fp.Decimal
			r.FloorPrice = &v
		}
		items = append(items, r)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "period": period, "sort": sort})
}

func rankOrder(sort string) string {
	switch sort {
	case "sales":
		return "COUNT(t.id) DESC, c.id"
	case "floor":
		return "c.floor_price NULLS LAST, c.id"
	default:
		return "SUM(t.price) DESC NULLS LAST, c.id"
	}
}
