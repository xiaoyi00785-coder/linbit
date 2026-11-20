package main

import (
	"context"
	"log"
	"time"

	"github.com/shopspring/decimal"
)

// expireListings cancels every active listing whose expires_at has passed and
// clears listed/list_price on the item. Also recomputes the collection's floor.
func (s *Server) expireListings(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, item_id, seller_id, price, asset FROM nft_listings
		 WHERE status='active' AND expires_at <= $1 ORDER BY id LIMIT 500`, s.now())
	if err != nil {
		return err
	}
	type expiring struct {
		id, itemID int64
		seller     string
		price      decimal.Decimal
		asset      string
	}
	var batch []expiring
	for rows.Next() {
		var e expiring
		if err := rows.Scan(&e.id, &e.itemID, &e.seller, &e.price, &e.asset); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, e)
	}
	rows.Close()
	colsTouched := map[int64]bool{}
	for _, e := range batch {
		if err := s.expireOne(ctx, "listing", e.id, e.itemID, e.seller, e.asset, e.price, colsTouched); err != nil {
			log.Printf("nft: expire listing %d: %v", e.id, err)
		}
	}
	return s.refreshTouched(ctx, colsTouched)
}

func (s *Server) expireOne(ctx context.Context, kind string, id, itemID int64,
	who, asset string, price decimal.Decimal, touched map[int64]bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var table string
	switch kind {
	case "listing":
		table = "nft_listings"
	case "offer":
		table = "nft_offers"
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE `+table+` SET status='expired', closed_at=$2 WHERE id=$1 AND status='active'`, id, s.now())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Commit()
	}
	it, err := s.loadItem(ctx, tx, itemID, true)
	if err != nil {
		return err
	}
	if kind == "listing" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE nft_items SET listed=false, list_price=NULL
			 WHERE id=$1 AND NOT EXISTS (
				SELECT 1 FROM nft_listings WHERE item_id=$1 AND status='active'
			 )`, itemID); err != nil {
			return err
		}
		if err := s.activity(ctx, tx, it, "listing_expired", who, "", &price, asset,
			map[string]any{"listingId": id}); err != nil {
			return err
		}
	} else {
		if err := s.activity(ctx, tx, it, "offer_expired", who, it.OwnerID, &price, asset,
			map[string]any{"offerId": id}); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	touched[it.CollectionID] = true
	return nil
}

// expireOffers mirrors expireListings for nft_offers.
func (s *Server) expireOffers(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, item_id, bidder_id, price, asset FROM nft_offers
		 WHERE status='active' AND expires_at <= $1 ORDER BY id LIMIT 500`, s.now())
	if err != nil {
		return err
	}
	type expiring struct {
		id, itemID int64
		bidder     string
		price      decimal.Decimal
		asset      string
	}
	var batch []expiring
	for rows.Next() {
		var e expiring
		if err := rows.Scan(&e.id, &e.itemID, &e.bidder, &e.price, &e.asset); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, e)
	}
	rows.Close()
	touched := map[int64]bool{}
	for _, e := range batch {
		if err := s.expireOne(ctx, "offer", e.id, e.itemID, e.bidder, e.asset, e.price, touched); err != nil {
			log.Printf("nft: expire offer %d: %v", e.id, err)
		}
	}
	return s.refreshTouched(ctx, touched)
}

func (s *Server) refreshTouched(ctx context.Context, touched map[int64]bool) error {
	if len(touched) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for cid := range touched {
		if err := s.recomputeCollection(ctx, tx, cid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// recomputeVolume refreshes volume_24h and sales_24h for every collection.
func (s *Server) recomputeVolume(ctx context.Context) error {
	cutoff := s.now().Add(-24 * time.Hour)
	_, err := s.db.ExecContext(ctx,
		`UPDATE nft_collections c SET
			volume_24h = COALESCE((SELECT SUM(price) FROM nft_trades t
				WHERE t.collection_id=c.id AND t.created_at >= $1), 0),
			sales_24h  = COALESCE((SELECT COUNT(*) FROM nft_trades t
				WHERE t.collection_id=c.id AND t.created_at >= $1), 0)`, cutoff)
	return err
}
