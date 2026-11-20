package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

func (s *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/nft/collections", s.internal(s.handleAdminCreateCollection))
	mux.HandleFunc("PATCH /internal/nft/collections/{slug}", s.internal(s.handleAdminPatchCollection))
	mux.HandleFunc("POST /internal/nft/items", s.internal(s.handleAdminMintItem))
	mux.HandleFunc("POST /internal/nft/items/{id}/burn", s.internal(s.handleAdminBurnItem))
}

type adminCollectionReq struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Banner      string `json:"banner"`
	CreatorID   string `json:"creatorId"`
	RoyaltyBps  int    `json:"royaltyBps"`
	PayAsset    string `json:"payAsset"`
	Category    string `json:"category"`
}

var slugAllowed = func(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}

func validateSlug(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !slugAllowed(r) {
			return false
		}
	}
	return true
}

func (s *Server) handleAdminCreateCollection(w http.ResponseWriter, r *http.Request) {
	var req adminCollectionReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	if !validateSlug(req.Slug) || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.CreatorID) == "" {
		writeErr(w, bad("VALIDATION_ERROR", "slug/name/creatorId 不合法"))
		return
	}
	if req.RoyaltyBps < 0 || req.RoyaltyBps > 2000 {
		writeErr(w, bad("VALIDATION_ERROR", "royaltyBps 范围 0-2000"))
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(req.PayAsset))
	if asset == "" {
		asset = s.cfg.PayAsset
	}
	ctx := r.Context()
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO nft_collections (slug, name, description, image, banner, creator_id, royalty_bps,
			pay_asset, category, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		req.Slug, req.Name, req.Description, req.Image, req.Banner, req.CreatorID,
		req.RoyaltyBps, asset, req.Category, s.now()).Scan(&id)
	if err != nil {
		if isPgUnique(err) {
			writeErr(w, conflict("SLUG_TAKEN", "slug 已被使用"))
			return
		}
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "slug": req.Slug})
}

func (s *Server) handleAdminPatchCollection(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Image       *string `json:"image"`
		Banner      *string `json:"banner"`
		Category    *string `json:"category"`
		RoyaltyBps  *int    `json:"royaltyBps"`
		Status      *string `json:"status"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	col, err := s.loadCollectionBySlug(r.Context(), s.db, slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	set := []string{}
	args := []any{col.ID}
	add := func(col string, v any) {
		args = append(args, v)
		set = append(set, col+"=$"+itoa(len(args)))
	}
	if req.Name != nil {
		add("name", *req.Name)
	}
	if req.Description != nil {
		add("description", *req.Description)
	}
	if req.Image != nil {
		add("image", *req.Image)
	}
	if req.Banner != nil {
		add("banner", *req.Banner)
	}
	if req.Category != nil {
		add("category", *req.Category)
	}
	if req.RoyaltyBps != nil {
		if *req.RoyaltyBps < 0 || *req.RoyaltyBps > 2000 {
			writeErr(w, bad("VALIDATION_ERROR", "royaltyBps 范围 0-2000"))
			return
		}
		add("royalty_bps", *req.RoyaltyBps)
	}
	if req.Status != nil {
		if *req.Status != "online" && *req.Status != "offline" {
			writeErr(w, bad("VALIDATION_ERROR", "status 不合法"))
			return
		}
		add("status", *req.Status)
	}
	if len(set) == 0 {
		common.WriteJSON(w, http.StatusOK, col)
		return
	}
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE nft_collections SET `+strings.Join(set, ", ")+` WHERE id=$1`, args...); err != nil {
		writeErr(w, err)
		return
	}
	col, err = s.loadCollection(r.Context(), s.db, col.ID, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, col)
}

type adminItemReq struct {
	CollectionSlug string          `json:"collectionSlug"`
	CollectionID   int64           `json:"collectionId"`
	TokenID        string          `json:"tokenId"`
	OwnerID        string          `json:"ownerId"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Image          string          `json:"image"`
	AnimationURL   string          `json:"animationUrl"`
	MetadataURI    string          `json:"metadataUri"`
	Attributes     json.RawMessage `json:"attributes"`
}

func (s *Server) handleAdminMintItem(w http.ResponseWriter, r *http.Request) {
	var req adminItemReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	if strings.TrimSpace(req.OwnerID) == "" || strings.TrimSpace(req.Name) == "" {
		writeErr(w, bad("VALIDATION_ERROR", "ownerId/name 不合法"))
		return
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	var col *Collection
	if req.CollectionSlug != "" {
		col, err = s.loadCollectionBySlug(ctx, tx, req.CollectionSlug)
	} else if req.CollectionID > 0 {
		col, err = s.loadCollection(ctx, tx, req.CollectionID, false)
	} else {
		writeErr(w, bad("VALIDATION_ERROR", "缺少 collectionSlug / collectionId"))
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	tokenID := strings.TrimSpace(req.TokenID)
	if tokenID == "" {
		// Auto-assign based on item_count. Readers see this as the on-chain token id.
		tokenID = itoa(col.ItemCount + 1)
	}
	attrs := req.Attributes
	if len(attrs) == 0 {
		attrs = json.RawMessage(`[]`)
	}
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO nft_items (collection_id, token_id, owner_id, name, description, image,
			animation_url, metadata_uri, attributes, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		col.ID, tokenID, req.OwnerID, req.Name, req.Description, req.Image,
		req.AnimationURL, req.MetadataURI, []byte(attrs), s.now()).Scan(&id)
	if err != nil {
		if isPgUnique(err) {
			writeErr(w, conflict("TOKEN_ID_TAKEN", "tokenId 已存在"))
			return
		}
		writeErr(w, err)
		return
	}
	it := &Item{ID: id, CollectionID: col.ID, OwnerID: req.OwnerID}
	price := decimal.Zero
	if err := s.activity(ctx, tx, it, "mint", "", req.OwnerID, &price, col.PayAsset,
		map[string]any{"tokenId": tokenID}); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.recomputeCollection(ctx, tx, col.ID); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "tokenId": tokenID, "collectionId": col.ID})
}

func (s *Server) handleAdminBurnItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	if it.Status == "burned" {
		common.WriteJSON(w, http.StatusOK, it)
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_items SET status='burned', listed=false, list_price=NULL WHERE id=$1`, id); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_listings SET status='cancelled', closed_at=$2 WHERE item_id=$1 AND status='active'`,
		id, s.now()); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_offers SET status='cancelled', closed_at=$2 WHERE item_id=$1 AND status='active'`,
		id, s.now()); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.activity(ctx, tx, it, "burn", it.OwnerID, "", nil, "", nil); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.recomputeCollection(ctx, tx, it.CollectionID); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": "burned"})
}

func isPgUnique(err error) bool {
	if err == nil {
		return false
	}
	// pgx returns a *pgconn.PgError with Code "23505" for unique_violation.
	return strings.Contains(err.Error(), "23505") || errors.Is(err, sql.ErrNoRows) == false &&
		strings.Contains(strings.ToLower(err.Error()), "unique")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
