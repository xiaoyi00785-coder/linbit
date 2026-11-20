package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

func (s *Server) activityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /nft/items/{id}/activity", s.handleItemActivity)
	mux.HandleFunc("GET /nft/collections/{slug}/activity", s.handleCollectionActivity)
}

type activityRow struct {
	ID           int64            `json:"id"`
	ItemID       int64            `json:"itemId"`
	CollectionID int64            `json:"collectionId"`
	Kind         string           `json:"kind"`
	From         string           `json:"from,omitempty"`
	To           string           `json:"to,omitempty"`
	Price        *decimal.Decimal `json:"price,omitempty"`
	Asset        string           `json:"asset,omitempty"`
	Detail       json.RawMessage  `json:"detail,omitempty"`
	At           int64            `json:"at"`
}

type sqlRows interface {
	Next() bool
	Scan(...any) error
}

func scanActivityRows(rows sqlRows) []activityRow {
	out := []activityRow{}
	for rows.Next() {
		var a activityRow
		var price decimal.NullDecimal
		var detail []byte
		var at time.Time
		if err := rows.Scan(&a.ID, &a.ItemID, &a.CollectionID, &a.Kind, &a.From, &a.To, &price, &a.Asset, &detail, &at); err != nil {
			return out
		}
		if price.Valid {
			v := price.Decimal
			a.Price = &v
		}
		a.Detail = detail
		a.At = at.UnixMilli()
		out = append(out, a)
	}
	return out
}

func (s *Server) handleItemActivity(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	limit, _ := pageParams(r)
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, item_id, collection_id, kind, from_uid, to_uid, price, asset, detail, at
		 FROM nft_activity WHERE item_id=$1 ORDER BY id DESC LIMIT $2`, id, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": scanActivityRows(rows)})
}

func (s *Server) handleCollectionActivity(w http.ResponseWriter, r *http.Request) {
	col, err := s.loadCollectionBySlug(r.Context(), s.db, r.PathValue("slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	limit, _ := pageParams(r)
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, item_id, collection_id, kind, from_uid, to_uid, price, asset, detail, at
		 FROM nft_activity WHERE collection_id=$1 ORDER BY id DESC LIMIT $2`, col.ID, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": scanActivityRows(rows)})
}

var _ = sql.ErrNoRows
