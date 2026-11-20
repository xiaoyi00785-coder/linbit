package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

func (s *Server) offerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /nft/items/{id}/offers", s.user(s.handleCreateOffer))
	mux.HandleFunc("POST /nft/offers/{id}/accept", s.user(s.handleAcceptOffer))
	mux.HandleFunc("DELETE /nft/offers/{id}", s.user(s.handleCancelOffer))
	mux.HandleFunc("GET /nft/offers/mine", s.user(s.handleMyOffers))
}

// topOffers returns the item's highest live offers, newest-first per price.
func (s *Server) topOffers(ctx context.Context, itemID int64, limit int) ([]*Offer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, item_id, bidder_id, price, asset, status, created_at, expires_at, closed_at
		 FROM nft_offers WHERE item_id=$1 AND status='active' AND expires_at > $2
		 ORDER BY price DESC, id DESC LIMIT $3`, itemID, time.Now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Offer{}
	for rows.Next() {
		o := &Offer{}
		if err := rows.Scan(&o.ID, &o.ItemID, &o.BidderID, &o.Price, &o.Asset,
			&o.Status, &o.CreatedAt, &o.ExpiresAt, &o.ClosedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *Server) handleCreateOffer(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Price       string `json:"price"`
		DurationDay int    `json:"durationDays"`
		Asset       string `json:"asset"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	price, err := parseAmount(req.Price)
	if err != nil {
		writeErr(w, err)
		return
	}
	if req.DurationDay <= 0 {
		req.DurationDay = 7
	}
	dur := time.Duration(req.DurationDay) * 24 * time.Hour
	if dur > s.cfg.OfferMaxDur {
		writeErr(w, bad("DURATION_TOO_LONG", "出价有效期超过上限"))
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
	if it.OwnerID == uid {
		writeErr(w, conflict("SELF_OFFER", "不能对自己的 NFT 出价"))
		return
	}
	if it.Status != "active" {
		writeErr(w, conflict("ITEM_INACTIVE", "NFT 已销毁或暂不可交易"))
		return
	}
	col, err := s.loadCollection(ctx, tx, it.CollectionID, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	asset := col.PayAsset
	if req.Asset != "" {
		asset = req.Asset
	}
	// One live offer per bidder per item: supersede the previous one.
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_offers SET status='cancelled', closed_at=$3
		 WHERE item_id=$1 AND bidder_id=$2 AND status='active'`,
		it.ID, uid, s.now()); err != nil {
		writeErr(w, err)
		return
	}
	now := s.now()
	exp := now.Add(dur)
	var oid int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO nft_offers (item_id, bidder_id, price, asset, status, created_at, expires_at)
		 VALUES ($1,$2,$3,$4,'active',$5,$6) RETURNING id`,
		it.ID, uid, price, asset, now, exp).Scan(&oid); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.activity(ctx, tx, it, "offer", uid, it.OwnerID, &price, asset,
		map[string]any{"offerId": oid, "expiresAt": exp.UnixMilli()}); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id": oid, "itemId": it.ID, "price": price, "asset": asset,
		"expiresAt": exp.UnixMilli(), "status": "active",
	})
}

func (s *Server) handleCancelOffer(w http.ResponseWriter, r *http.Request, uid string) {
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
	var bidder, status, asset string
	var itemID int64
	var price decimal.Decimal
	err = tx.QueryRowContext(ctx,
		`SELECT item_id, bidder_id, price, asset, status FROM nft_offers WHERE id=$1 FOR UPDATE`, id).
		Scan(&itemID, &bidder, &price, &asset, &status)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, notFound("OFFER_NOT_FOUND", "出价不存在"))
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	if bidder != uid {
		writeErr(w, forbidden("NOT_BIDDER", "只有出价人可撤销"))
		return
	}
	if status != "active" {
		writeErr(w, conflict("OFFER_CLOSED", "出价已结束"))
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_offers SET status='cancelled', closed_at=$2 WHERE id=$1`, id, s.now()); err != nil {
		writeErr(w, err)
		return
	}
	it, err := s.loadItem(ctx, tx, itemID, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.activity(ctx, tx, it, "offer_cancel", uid, it.OwnerID, &price, asset,
		map[string]any{"offerId": id}); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": "cancelled"})
}

func (s *Server) handleAcceptOffer(w http.ResponseWriter, r *http.Request, uid string) {
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
	var o Offer
	err = tx.QueryRowContext(ctx,
		`SELECT id, item_id, bidder_id, price, asset, status, created_at, expires_at, closed_at
		 FROM nft_offers WHERE id=$1 FOR UPDATE`, id).
		Scan(&o.ID, &o.ItemID, &o.BidderID, &o.Price, &o.Asset,
			&o.Status, &o.CreatedAt, &o.ExpiresAt, &o.ClosedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, notFound("OFFER_NOT_FOUND", "出价不存在"))
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	if o.Status != "active" {
		writeErr(w, conflict("OFFER_CLOSED", "出价已结束"))
		return
	}
	if !o.ExpiresAt.After(s.now()) {
		writeErr(w, conflict("OFFER_EXPIRED", "出价已过期"))
		return
	}
	it, err := s.loadItem(ctx, tx, o.ItemID, true)
	if err != nil {
		writeErr(w, err)
		return
	}
	if it.OwnerID != uid {
		writeErr(w, forbidden("NOT_OWNER", "只有持有人可接受出价"))
		return
	}
	if it.OwnerID == o.BidderID {
		writeErr(w, conflict("SELF_OFFER", "不能接受自己的出价"))
		return
	}
	col, err := s.loadCollection(ctx, tx, it.CollectionID, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	royalty := floor8(o.Price.Mul(decimal.NewFromInt(int64(col.RoyaltyBps))).Div(decimal.NewFromInt(10000)))
	if royalty.Sign() < 0 || royalty.GreaterThanOrEqual(o.Price) {
		royalty = decimal.Zero
	}
	sellerCut := o.Price.Sub(royalty)
	payCreator := royalty.Sign() > 0 && col.CreatorID != "" && col.CreatorID != uid && col.CreatorID != o.BidderID
	// Reuse settleTrade by faking a listing row (seller = current owner).
	fakeListing := &Listing{ID: o.ID, ItemID: it.ID, SellerID: uid, Price: o.Price, Asset: o.Asset}
	if err := s.settleTrade(ctx, tx, it, col, fakeListing, o.BidderID, "offer", o.ID, o.Price, sellerCut, royalty, payCreator); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_offers SET status='accepted', closed_at=$2 WHERE id=$1`, o.ID, s.now()); err != nil {
		writeErr(w, err)
		return
	}
	// Any live listing on this item is now stale; cancel it.
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_listings SET status='cancelled', closed_at=$2 WHERE item_id=$1 AND status='active'`,
		it.ID, s.now()); err != nil {
		writeErr(w, err)
		return
	}
	// Cancel any live offers from the former owner (now the seller) and from
	// the new owner (they just took the asset).
	if err := s.closeOffersAround(ctx, tx, it.ID, o.BidderID, uid); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id": o.ID, "itemId": it.ID, "price": o.Price, "asset": o.Asset,
		"royaltyAmount": royalty, "royaltyTo": col.CreatorID, "sellerAmount": sellerCut,
	})
}

func (s *Server) handleMyOffers(w http.ResponseWriter, r *http.Request, uid string) {
	role := r.URL.Query().Get("role") // bidder | received
	limit, offset := pageParams(r)
	var query string
	var args []any
	switch role {
	case "received":
		query = `SELECT o.id, o.item_id, o.bidder_id, o.price, o.asset, o.status, o.created_at, o.expires_at, o.closed_at
			FROM nft_offers o JOIN nft_items i ON i.id=o.item_id
			WHERE i.owner_id=$1 AND o.status='active' AND o.expires_at > $2
			ORDER BY o.price DESC, o.id DESC LIMIT $3 OFFSET $4`
		args = []any{uid, s.now(), limit, offset}
	default:
		query = `SELECT id, item_id, bidder_id, price, asset, status, created_at, expires_at, closed_at
			FROM nft_offers WHERE bidder_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`
		args = []any{uid, limit, offset}
	}
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []*Offer{}
	for rows.Next() {
		o := &Offer{}
		if err := rows.Scan(&o.ID, &o.ItemID, &o.BidderID, &o.Price, &o.Asset,
			&o.Status, &o.CreatedAt, &o.ExpiresAt, &o.ClosedAt); err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, o)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out, "nextOffset": offset + len(out)})
}

var _ = fmt.Sprint
