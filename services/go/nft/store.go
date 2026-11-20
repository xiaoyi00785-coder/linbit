package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

type Collection struct {
	ID           int64            `json:"id"`
	Slug         string           `json:"slug"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Image        string           `json:"image"`
	Banner       string           `json:"banner"`
	CreatorID    string           `json:"creatorId"`
	RoyaltyBps   int              `json:"royaltyBps"`
	PayAsset     string           `json:"payAsset"`
	Category     string           `json:"category"`
	ItemCount    int              `json:"itemCount"`
	OwnerCount   int              `json:"ownerCount"`
	FloorPrice   *decimal.Decimal `json:"floorPrice,omitempty"`
	VolumeTotal  decimal.Decimal  `json:"volumeTotal"`
	Volume24h    decimal.Decimal  `json:"volume24h"`
	SalesTotal   int64            `json:"salesTotal"`
	Sales24h     int64            `json:"sales24h"`
	Status       string           `json:"status"`
	CreatedAt    time.Time        `json:"createdAt"`
}

const collectionCols = `id, slug, name, description, image, banner, creator_id, royalty_bps, pay_asset, category,
	item_count, owner_count, floor_price, volume_total, volume_24h, sales_total, sales_24h, status, created_at`

type rowScanner interface {
	Scan(...any) error
}

func scanCollection(r rowScanner) (*Collection, error) {
	c := &Collection{}
	var fp decimal.NullDecimal
	if err := r.Scan(&c.ID, &c.Slug, &c.Name, &c.Description, &c.Image, &c.Banner, &c.CreatorID, &c.RoyaltyBps,
		&c.PayAsset, &c.Category, &c.ItemCount, &c.OwnerCount, &fp, &c.VolumeTotal, &c.Volume24h,
		&c.SalesTotal, &c.Sales24h, &c.Status, &c.CreatedAt); err != nil {
		return nil, err
	}
	if fp.Valid {
		v := fp.Decimal
		c.FloorPrice = &v
	}
	return c, nil
}

func (s *Server) loadCollectionBySlug(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, slug string) (*Collection, error) {
	c, err := scanCollection(q.QueryRowContext(ctx, `SELECT `+collectionCols+` FROM nft_collections WHERE slug=$1`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("COLLECTION_NOT_FOUND", "合集不存在")
	}
	return c, err
}

func (s *Server) loadCollection(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int64, lock bool) (*Collection, error) {
	sfx := ""
	if lock {
		sfx = " FOR UPDATE"
	}
	c, err := scanCollection(q.QueryRowContext(ctx, `SELECT `+collectionCols+` FROM nft_collections WHERE id=$1`+sfx, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("COLLECTION_NOT_FOUND", "合集不存在")
	}
	return c, err
}

type Item struct {
	ID            int64            `json:"id"`
	CollectionID  int64            `json:"collectionId"`
	TokenID       string           `json:"tokenId"`
	OwnerID       string           `json:"ownerId"`
	Name          string           `json:"name"`
	Description   string           `json:"description"`
	Image         string           `json:"image"`
	AnimationURL  string           `json:"animationUrl,omitempty"`
	MetadataURI   string           `json:"metadataUri,omitempty"`
	Attributes    json.RawMessage  `json:"attributes"`
	Listed        bool             `json:"listed"`
	ListPrice     *decimal.Decimal `json:"listPrice,omitempty"`
	LastSalePrice *decimal.Decimal `json:"lastSalePrice,omitempty"`
	FavoriteCount int              `json:"favoriteCount"`
	Status        string           `json:"status"`
	CreatedAt     time.Time        `json:"createdAt"`
}

const itemCols = `id, collection_id, token_id, owner_id, name, description, image, animation_url, metadata_uri,
	attributes, listed, list_price, last_sale_price, favorite_count, status, created_at`

func scanItem(r rowScanner) (*Item, error) {
	it := &Item{}
	var lp, lsp decimal.NullDecimal
	var attrs []byte
	if err := r.Scan(&it.ID, &it.CollectionID, &it.TokenID, &it.OwnerID, &it.Name, &it.Description,
		&it.Image, &it.AnimationURL, &it.MetadataURI, &attrs, &it.Listed, &lp, &lsp,
		&it.FavoriteCount, &it.Status, &it.CreatedAt); err != nil {
		return nil, err
	}
	if len(attrs) == 0 {
		attrs = []byte("[]")
	}
	it.Attributes = attrs
	if lp.Valid {
		v := lp.Decimal
		it.ListPrice = &v
	}
	if lsp.Valid {
		v := lsp.Decimal
		it.LastSalePrice = &v
	}
	return it, nil
}

func (s *Server) loadItem(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int64, lock bool) (*Item, error) {
	sfx := ""
	if lock {
		sfx = " FOR UPDATE"
	}
	it, err := scanItem(q.QueryRowContext(ctx, `SELECT `+itemCols+` FROM nft_items WHERE id=$1`+sfx, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("ITEM_NOT_FOUND", "NFT 不存在")
	}
	return it, err
}

type Listing struct {
	ID        int64           `json:"id"`
	ItemID    int64           `json:"itemId"`
	SellerID  string          `json:"sellerId"`
	Price     decimal.Decimal `json:"price"`
	Asset     string          `json:"asset"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"createdAt"`
	ExpiresAt time.Time       `json:"expiresAt"`
	ClosedAt  *time.Time      `json:"closedAt,omitempty"`
}

type Offer struct {
	ID        int64           `json:"id"`
	ItemID    int64           `json:"itemId"`
	BidderID  string          `json:"bidderId"`
	Price     decimal.Decimal `json:"price"`
	Asset     string          `json:"asset"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"createdAt"`
	ExpiresAt time.Time       `json:"expiresAt"`
	ClosedAt  *time.Time      `json:"closedAt,omitempty"`
}

// activity writes one row into nft_activity. detail may be nil.
func (s *Server) activity(ctx context.Context, tx *sql.Tx, item *Item, kind, from, to string, price *decimal.Decimal, asset string, detail map[string]any) error {
	d := []byte("{}")
	if detail != nil {
		d, _ = json.Marshal(detail)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO nft_activity
		(item_id, collection_id, kind, from_uid, to_uid, price, asset, detail, at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		item.ID, item.CollectionID, kind, from, to, price, asset, d, s.now())
	return err
}

// recomputeCollectionAggregates refreshes floor_price, item_count, owner_count
// and total volume/sales for a single collection row.
func (s *Server) recomputeCollection(ctx context.Context, tx *sql.Tx, cid int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE nft_collections c SET
		item_count   = (SELECT COUNT(*) FROM nft_items i WHERE i.collection_id=c.id AND i.status='active'),
		owner_count  = (SELECT COUNT(DISTINCT owner_id) FROM nft_items i WHERE i.collection_id=c.id AND i.status='active'),
		floor_price  = (SELECT MIN(list_price) FROM nft_items i WHERE i.collection_id=c.id AND i.status='active' AND i.listed),
		volume_total = COALESCE((SELECT SUM(price) FROM nft_trades t WHERE t.collection_id=c.id), 0),
		sales_total  = COALESCE((SELECT COUNT(*) FROM nft_trades t WHERE t.collection_id=c.id), 0)
		WHERE c.id=$1`, cid)
	return err
}
