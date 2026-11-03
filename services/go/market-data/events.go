package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Sink consumes engine output events. The aggregator implements it. The
// transport that feeds the sink is replaceable (see HTTPEventSource).
type Sink interface {
	OnTrade(TradeEvent)
	OnDepth(DepthEvent)
}

// TradeEvent is a matched trade observed on the event stream. The engine does
// not stamp trades with a time, so the consumer stamps them on receipt.
type TradeEvent struct {
	TradeID   string
	Symbol    string
	Price     decimal.Decimal
	Quantity  decimal.Decimal
	TakerSide string // buy | sell — the aggressor's side
	Time      time.Time
}

// Level is one price level of a depth snapshot/delta.
type Level struct {
	Price    decimal.Decimal
	Quantity decimal.Decimal
}

// DepthEvent is an order-book snapshot or delta. For a delta a quantity of 0
// means the level was removed.
type DepthEvent struct {
	Symbol   string
	Sequence uint64
	Snapshot bool
	Bids     []Level
	Asks     []Level
}

// wireEvent mirrors the engine's EngineEvent JSON (see
// services/rust/matching-engine/src/types.rs). The `event` tag selects which
// fields are populated: "trade" or "depth".
type wireEvent struct {
	Event string `json:"event"`
	// trade fields
	TradeID   string `json:"tradeId"`
	Symbol    string `json:"symbol"`
	Price     string `json:"price"`
	Quantity  string `json:"quantity"`
	TakerSide string `json:"takerSide"`
	// depth fields
	Sequence uint64      `json:"sequence"`
	Snapshot bool        `json:"snapshot"`
	Bids     []wireLevel `json:"bids"`
	Asks     []wireLevel `json:"asks"`
}

type wireLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

// HTTPEventSource is the local implementation of the replaceable inbound event
// transport. The matching engine's HttpSink POSTs one JSON event per request to
// this handler (the stand-in for a durable pub/sub stream); a production
// deployment would instead consume the same events from Kafka and call the same
// Sink, without touching the aggregator. Events are best-effort: a decode error
// is logged and acknowledged (the engine does not retry), and settlement never
// depends on this path.
type HTTPEventSource struct {
	sink  Sink
	token string
	now   func() time.Time
}

// NewHTTPEventSource builds the ingest handler.
func NewHTTPEventSource(sink Sink, token string) *HTTPEventSource {
	return &HTTPEventSource{sink: sink, token: token, now: time.Now}
}

// Handler returns the HTTP handler that receives engine events.
func (s *HTTPEventSource) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && r.Header.Get("X-Internal-Token") != s.token {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "无效的内部令牌")
			return
		}
		var ev wireEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			log.Printf("ingest: decode event: %v", err)
			// Acknowledge: the engine does not retry and must not block matching.
			w.WriteHeader(http.StatusOK)
			return
		}
		s.dispatch(ev)
		w.WriteHeader(http.StatusOK)
	}
}

// dispatch converts a wire event to a typed event and forwards it to the sink.
func (s *HTTPEventSource) dispatch(ev wireEvent) {
	switch ev.Event {
	case "trade":
		price, err1 := decimal.NewFromString(ev.Price)
		qty, err2 := decimal.NewFromString(ev.Quantity)
		if err1 != nil || err2 != nil {
			log.Printf("ingest: bad trade amounts price=%q qty=%q", ev.Price, ev.Quantity)
			return
		}
		s.sink.OnTrade(TradeEvent{
			TradeID:   ev.TradeID,
			Symbol:    ev.Symbol,
			Price:     price,
			Quantity:  qty,
			TakerSide: ev.TakerSide,
			Time:      s.now(),
		})
	case "depth":
		s.sink.OnDepth(DepthEvent{
			Symbol:   ev.Symbol,
			Sequence: ev.Sequence,
			Snapshot: ev.Snapshot,
			Bids:     toLevels(ev.Bids),
			Asks:     toLevels(ev.Asks),
		})
	default:
		log.Printf("ingest: unknown event %q", ev.Event)
	}
}

func toLevels(in []wireLevel) []Level {
	out := make([]Level, 0, len(in))
	for _, l := range in {
		price, err1 := decimal.NewFromString(l.Price)
		qty, err2 := decimal.NewFromString(l.Quantity)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, Level{Price: price, Quantity: qty})
	}
	return out
}
