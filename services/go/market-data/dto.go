package main

// DTOs serialized by both the REST handlers and the WebSocket pushes. Field
// names follow docs/api/openapi.yaml and docs/api/websocket.md so the same
// shapes feed every client generator.

// tickerDTO is the 24h rolling ticker (openapi Ticker).
type tickerDTO struct {
	Symbol             string `json:"symbol"`
	LastPrice          string `json:"lastPrice"`
	PriceChangePercent string `json:"priceChangePercent"`
	High               string `json:"high"`
	Low                string `json:"low"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
}

// depthDTO is an order-book snapshot or delta (openapi Depth / ws depth).
type depthDTO struct {
	Symbol       string      `json:"symbol"`
	LastUpdateID uint64      `json:"lastUpdateId"`
	Bids         [][2]string `json:"bids"`
	Asks         [][2]string `json:"asks"`
}

// restKline is a candle for GET /market/klines (openapi Kline).
type restKline struct {
	Time   int64  `json:"time"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
}

// wsKline is a candle for the kline WebSocket channel (adds `closed`).
type wsKline struct {
	Time   int64  `json:"time"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
	Closed bool   `json:"closed"`
}

// tradeDTO is a public trade (ws trades channel + REST recent trades).
type tradeDTO struct {
	TradeID  string `json:"tradeId"`
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
	Side     string `json:"side"`
	Time     int64  `json:"time"`
}

// symbolDTO is a trading pair (openapi Symbol).
type symbolDTO struct {
	Symbol            string   `json:"symbol"`
	Base              string   `json:"base"`
	Quote             string   `json:"quote"`
	PricePrecision    int      `json:"pricePrecision"`
	QuantityPrecision int      `json:"quantityPrecision"`
	MinQuantity       *string  `json:"minQuantity,omitempty"`
	Status            string   `json:"status"`
	Tags              []string `json:"tags"` // market_categories ids containing this symbol
}

func toRESTKline(c Candle) restKline {
	return restKline{
		Time:   c.OpenTime,
		Open:   c.Open.String(),
		High:   c.High.String(),
		Low:    c.Low.String(),
		Close:  c.Close.String(),
		Volume: c.Volume.String(),
	}
}

func toWSKline(c Candle) wsKline {
	return wsKline{
		Time:   c.OpenTime,
		Open:   c.Open.String(),
		High:   c.High.String(),
		Low:    c.Low.String(),
		Close:  c.Close.String(),
		Volume: c.Volume.String(),
		Closed: c.Closed,
	}
}
