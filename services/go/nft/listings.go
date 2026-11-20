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

func (s *Server) listingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /nft/items/{id}/listings", s.user(s.handleCreateListing))
	mux.HandleFunc("DELETE /nft/listings/{id}", s.user(s.handleCancelListing))
	mux.HandleFunc("POST /nft/items/{id}/buy", s.user(s.handleBuy))
}

// activeListing returns the item's live listing or nil. Not authoritative — use
// FOR UPDATE inside buy/cancel.
func (s *Server) activeListing(ctx context.Context, itemID int64) *Listing {
	var l Listing
	err := s.db.QueryRowContext(ctx,
		`SELECT id, item_id, seller_id, price, asset, status, created_at, expires_at, closed_at
		 FROM nft_listings WHERE item_id=$1 AND status='active'
		 ORDER BY id DESC LIMIT 1`, itemID).
		Scan(&l.ID, &l.ItemID, &l.SellerID, &l.Price, &l.Asset, &l.Status, &l.CreatedAt, &l.ExpiresAt, &l.ClosedAt)
	if err != nil {
		return nil
	}
	return &l
}

type listingReq struct {
	Price       string `json:"price"`
	DurationDay int    `json:"durationDays"`
	Asset       string `json:"asset"`
}

func (s *Server) handleCreateListing(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var req listingReq
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
		req.DurationDay = 30
	}
	dur := time.Duration(req.DurationDay) * 24 * time.Hour
	if dur > s.cfg.ListingMaxDur {
		writeErr(w, bad("DURATION_TOO_LONG", "挂单时长超过上限"))
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
	if it.Status != "active" {
		writeErr(w, conflict("ITEM_INACTIVE", "NFT 已销毁或暂不可交易"))
		return
	}
	if it.OwnerID != uid {
		writeErr(w, forbidden("NOT_OWNER", "只有持有人可挂单"))
		return
	}
	// Cancel any live listing silently — one active listing per item.
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_listings SET status='cancelled', closed_at=$2 WHERE item_id=$1 AND status='active'`,
		it.ID, s.now()); err != nil {
		writeErr(w, err)
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
	now := s.now()
	exp := now.Add(dur)
	var lid int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO nft_listings (item_id, seller_id, price, asset, status, created_at, expires_at)
		 VALUES ($1,$2,$3,$4,'active',$5,$6) RETURNING id`,
		it.ID, uid, price, asset, now, exp).Scan(&lid); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_items SET listed=true, list_price=$2 WHERE id=$1`, it.ID, price); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.activity(ctx, tx, it, "list", uid, "", &price, asset,
		map[string]any{"listingId": lid, "expiresAt": exp.UnixMilli()}); err != nil {
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
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id": lid, "itemId": it.ID, "price": price, "asset": asset,
		"expiresAt": exp.UnixMilli(), "status": "active",
	})
}

func (s *Server) handleCancelListing(w http.ResponseWriter, r *http.Request, uid string) {
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
	var seller, status, asset string
	var itemID int64
	var price decimal.Decimal
	err = tx.QueryRowContext(ctx,
		`SELECT item_id, seller_id, price, asset, status FROM nft_listings WHERE id=$1 FOR UPDATE`, id).
		Scan(&itemID, &seller, &price, &asset, &status)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, notFound("LISTING_NOT_FOUND", "挂单不存在"))
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	if seller != uid {
		writeErr(w, forbidden("NOT_OWNER", "只有卖家可撤单"))
		return
	}
	if status != "active" {
		writeErr(w, conflict("LISTING_CLOSED", "挂单已结束"))
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_listings SET status='cancelled', closed_at=$2 WHERE id=$1`, id, s.now()); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_items SET listed=false, list_price=NULL WHERE id=$1`, itemID); err != nil {
		writeErr(w, err)
		return
	}
	it, err := s.loadItem(ctx, tx, itemID, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.activity(ctx, tx, it, "unlist", uid, "", &price, asset,
		map[string]any{"listingId": id}); err != nil {
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
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": "cancelled"})
}

func (s *Server) handleBuy(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		ListingID int64  `json:"listingId"` // optional guard against race on price
		MaxPrice  string `json:"maxPrice"`
	}
	_ = common.DecodeJSON(r, &req)
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.buyItem(r.Context(), uid, id, req.ListingID, req.MaxPrice)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

// buyItem settles an active listing: ownership transfers to the buyer and the
// asset service moves price-royalty to the seller and royalty to the collection
// creator. Caller holds s.mu.
func (s *Server) buyItem(ctx context.Context, buyer string, itemID, listingID int64, maxPrice string) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	it, err := s.loadItem(ctx, tx, itemID, true)
	if err != nil {
		return nil, err
	}
	if it.OwnerID == buyer {
		return nil, conflict("SELF_BUY", "不能购买自己的 NFT")
	}
	var l Listing
	err = tx.QueryRowContext(ctx,
		`SELECT id, item_id, seller_id, price, asset, status, created_at, expires_at, closed_at
		 FROM nft_listings WHERE item_id=$1 AND status='active' ORDER BY id DESC LIMIT 1 FOR UPDATE`, it.ID).
		Scan(&l.ID, &l.ItemID, &l.SellerID, &l.Price, &l.Asset, &l.Status, &l.CreatedAt, &l.ExpiresAt, &l.ClosedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("LISTING_NOT_FOUND", "NFT 未在出售")
	}
	if err != nil {
		return nil, err
	}
	if listingID != 0 && listingID != l.ID {
		return nil, conflict("LISTING_CHANGED", "挂单已更新，请刷新")
	}
	if l.SellerID != it.OwnerID {
		return nil, conflict("LISTING_STALE", "挂单与持有人不一致，请刷新")
	}
	if !l.ExpiresAt.After(s.now()) {
		return nil, conflict("LISTING_EXPIRED", "挂单已过期")
	}
	if maxPrice != "" {
		mp, err := parseAmount(maxPrice)
		if err != nil {
			return nil, err
		}
		if l.Price.GreaterThan(mp) {
			return nil, conflict("PRICE_CHANGED", "成交价已高于限价")
		}
	}
	col, err := s.loadCollection(ctx, tx, it.CollectionID, false)
	if err != nil {
		return nil, err
	}
	royalty := floor8(l.Price.Mul(decimal.NewFromInt(int64(col.RoyaltyBps))).Div(decimal.NewFromInt(10000)))
	if royalty.Sign() < 0 {
		royalty = decimal.Zero
	}
	if royalty.GreaterThanOrEqual(l.Price) {
		royalty = decimal.Zero // malformed royalty_bps — skip
	}
	sellerCut := l.Price.Sub(royalty)
	payCreator := royalty.Sign() > 0 && col.CreatorID != "" && col.CreatorID != l.SellerID && col.CreatorID != buyer
	if err := s.settleTrade(ctx, tx, it, col, &l, buyer, "listing", l.ID, l.Price, sellerCut, royalty, payCreator); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_listings SET status='sold', closed_at=$2 WHERE id=$1`, l.ID, s.now()); err != nil {
		return nil, err
	}
	// Cancel any active offers from this buyer or from the former owner.
	if err := s.closeOffersAround(ctx, tx, it.ID, buyer, l.SellerID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{
		"itemId": it.ID, "listingId": l.ID, "price": l.Price, "asset": l.Asset,
		"royaltyAmount": royalty, "royaltyTo": col.CreatorID, "sellerAmount": sellerCut,
	}, nil
}

// settleTrade moves money, records the trade and transfers ownership.
// payCreator=false when royalty is zero or the creator is the seller/buyer.
func (s *Server) settleTrade(
	ctx context.Context, tx *sql.Tx, it *Item, col *Collection, l *Listing,
	buyer, source string, sourceID int64,
	price, sellerCut, royalty decimal.Decimal, payCreator bool,
) error {
	seller := l.SellerID
	royaltyRecipient := ""
	if payCreator {
		royaltyRecipient = col.CreatorID
	}
	// Idempotent references scoped per leg; a retried commit of the same source
	// hits ErrAlreadyApplied downstream and the ledger stays balanced.
	refBase := fmt.Sprintf("nft:%s:%d", source, sourceID)
	if sellerCut.Sign() > 0 {
		if err := s.userTransfer(ctx, buyer, seller, l.Asset, sellerCut, refBase+":seller"); err != nil {
			return err
		}
	}
	if payCreator {
		if err := s.userTransfer(ctx, buyer, col.CreatorID, l.Asset, royalty, refBase+":royalty"); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO nft_trades (item_id, collection_id, seller_id, buyer_id, price, asset,
			royalty_amount, royalty_to, source, source_id, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		it.ID, it.CollectionID, seller, buyer, price, l.Asset, royalty, royaltyRecipient,
		source, sourceID, s.now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE nft_items SET owner_id=$2, listed=false, list_price=NULL, last_sale_price=$3 WHERE id=$1`,
		it.ID, buyer, price); err != nil {
		return err
	}
	it.OwnerID = buyer // activity writer sees fresh state
	detail := map[string]any{"listingId": l.ID, "source": source,
		"sellerAmount": sellerCut, "royaltyAmount": royalty, "royaltyTo": royaltyRecipient}
	if err := s.activity(ctx, tx, it, "sale", seller, buyer, &price, l.Asset, detail); err != nil {
		return err
	}
	if err := s.activity(ctx, tx, it, "transfer", seller, buyer, nil, "", detail); err != nil {
		return err
	}
	return s.recomputeCollection(ctx, tx, it.CollectionID)
}

// closeOffersAround cancels every live offer where the bidder is now the new
// owner (they got what they wanted) or where the bidder is the former seller.
func (s *Server) closeOffersAround(ctx context.Context, tx *sql.Tx, itemID int64, buyer, seller string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE nft_offers SET status='cancelled', closed_at=$2
		 WHERE item_id=$1 AND status='active' AND (bidder_id=$3 OR bidder_id=$4)`,
		itemID, s.now(), buyer, seller)
	return err
}

func parseAmount(raw string) (decimal.Decimal, error) {
	a, err := decimal.NewFromString(raw)
	if err != nil || a.Sign() <= 0 || a.Exponent() < -dp {
		return decimal.Zero, bad("VALIDATION_ERROR", "金额不合法")
	}
	return a, nil
}
