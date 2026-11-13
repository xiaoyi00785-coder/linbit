package main

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// kafkaBufCap is how many result-stream entries we retain per symbol. Older
// seqs are dropped; a gap is filled from the engine's HTTP stream, which is
// still the durable log.
const kafkaBufCap = 8192

// KafkaReportStream consumes the engine result topic and serves StreamPage
// from that buffer when the next seq is present. Missing seqs and acks go
// through the HTTP stream, so a dropped produce cannot lose a fill.
type KafkaReportStream struct {
	brokers []string
	topic   string
	inner   *HTTPStream

	mu  sync.Mutex
	buf map[string]map[int64]StreamEntry

	wmMu sync.Mutex
	wm   map[string]wmSample
}

// wmSample is an engine-clock observation. Later reads estimate the engine's
// current time as engineMs + time since the sample, so a stale sample does not
// look like clock skew.
type wmSample struct {
	engineMs int64
	localMs  int64
}

// NewKafkaReportStream constructs a consumer. Call Run to start reading.
func NewKafkaReportStream(brokers []string, topic string, inner *HTTPStream) *KafkaReportStream {
	if topic == "" {
		topic = "engine.reports"
	}
	return &KafkaReportStream{
		brokers: brokers,
		topic:   topic,
		inner:   inner,
		buf:     map[string]map[int64]StreamEntry{},
		wm:      map[string]wmSample{},
	}
}

// Run reads every partition until ctx is cancelled. Each process reads the
// full topic (no consumer group): the cursor lease, not the group, decides
// which replica settles a symbol.
func (k *KafkaReportStream) Run(ctx context.Context) {
	started := map[int]struct{}{}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	var wg sync.WaitGroup
	for {
		ids, err := k.partitions(ctx)
		if err != nil {
			log.Printf("kafka: list partitions %s: %v", k.topic, err)
		}
		for _, id := range ids {
			if _, ok := started[id]; ok {
				continue
			}
			started[id] = struct{}{}
			wg.Add(1)
			go func(partition int) {
				defer wg.Done()
				k.readPartition(ctx, partition)
			}(id)
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-tick.C:
		}
	}
}

func (k *KafkaReportStream) partitions(ctx context.Context) ([]int, error) {
	if len(k.brokers) == 0 {
		return nil, errNoBrokers
	}
	dialer := &kafka.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", k.brokers[0])
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	parts, err := conn.ReadPartitions(k.topic)
	if err != nil {
		return nil, err
	}
	seen := map[int]struct{}{}
	var ids []int
	for _, p := range parts {
		if p.Topic != k.topic {
			continue
		}
		if _, ok := seen[p.ID]; ok {
			continue
		}
		seen[p.ID] = struct{}{}
		ids = append(ids, p.ID)
	}
	sort.Ints(ids)
	return ids, nil
}

func (k *KafkaReportStream) readPartition(ctx context.Context, partition int) {
	for ctx.Err() == nil {
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:     k.brokers,
			Topic:       k.topic,
			Partition:   partition,
			StartOffset: kafka.LastOffset,
			MaxBytes:    1 << 20,
			MaxWait:     200 * time.Millisecond,
		})
		err := k.consume(ctx, r)
		_ = r.Close()
		if ctx.Err() != nil {
			return
		}
		log.Printf("kafka: partition %d: %v", partition, err)
		sleepCtx(ctx, time.Second)
	}
}

func (k *KafkaReportStream) consume(ctx context.Context, r *kafka.Reader) error {
	for {
		msg, err := r.ReadMessage(ctx)
		if err != nil {
			return err
		}
		var e StreamEntry
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			log.Printf("kafka: bad record partition %d offset %d: %v", msg.Partition, msg.Offset, err)
			continue
		}
		sym := string(msg.Key)
		if sym == "" {
			sym = e.Report.Symbol
		}
		k.add(sym, e)
	}
}

func (k *KafkaReportStream) add(symbol string, e StreamEntry) {
	if symbol == "" || e.Seq <= 0 {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	m := k.buf[symbol]
	if m == nil {
		m = map[int64]StreamEntry{}
		k.buf[symbol] = m
	}
	m[e.Seq] = e
	if len(m) <= kafkaBufCap {
		return
	}
	seqs := make([]int64, 0, len(m))
	for s := range m {
		seqs = append(seqs, s)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for _, s := range seqs[:len(m)-kafkaBufCap] {
		delete(m, s)
	}
}

// contiguous returns the longest prefix of entries whose seqs are after+1,
// after+2, ... up to limit. A hole stops the prefix so the caller can fill it
// from HTTP instead of skipping a fill.
func (k *KafkaReportStream) contiguous(symbol string, after int64, limit int) []StreamEntry {
	k.mu.Lock()
	defer k.mu.Unlock()
	m := k.buf[symbol]
	var out []StreamEntry
	next := after + 1
	for len(out) < limit {
		e, ok := m[next]
		if !ok {
			break
		}
		out = append(out, e)
		next++
	}
	return out
}

// StreamPage implements EngineStream.
func (k *KafkaReportStream) StreamPage(ctx context.Context, symbol string, after int64, limit int) (*StreamPageResponse, error) {
	entries := k.contiguous(symbol, after, limit)
	if len(entries) > 0 && entries[0].Seq == after+1 {
		wm := k.watermark(ctx, symbol, after)
		if wm == 0 {
			wm = entries[len(entries)-1].Ts
		}
		return &StreamPageResponse{Entries: entries, Head: entries[len(entries)-1].Seq, Watermark: wm}, nil
	}
	page, err := k.inner.StreamPage(ctx, symbol, after, limit)
	if err != nil {
		return nil, err
	}
	k.noteWatermark(symbol, page.Watermark)
	return page, nil
}

// Ack implements EngineStream. The engine still trims its log on the HTTP ack.
func (k *KafkaReportStream) Ack(ctx context.Context, symbol string, seq int64) error {
	return k.inner.Ack(ctx, symbol, seq)
}

func (k *KafkaReportStream) watermark(ctx context.Context, symbol string, after int64) int64 {
	k.wmMu.Lock()
	s, ok := k.wm[symbol]
	k.wmMu.Unlock()
	if !ok || time.Since(time.UnixMilli(s.localMs)) >= 5*time.Second {
		page, err := k.inner.StreamPage(ctx, symbol, after, 1)
		if err == nil {
			k.noteWatermark(symbol, page.Watermark)
			s = wmSample{engineMs: page.Watermark, localMs: time.Now().UnixMilli()}
		}
	}
	if s.engineMs <= 0 {
		return 0
	}
	return s.engineMs + (time.Now().UnixMilli() - s.localMs)
}

func (k *KafkaReportStream) noteWatermark(symbol string, watermark int64) {
	if watermark <= 0 {
		return
	}
	k.wmMu.Lock()
	k.wm[symbol] = wmSample{engineMs: watermark, localMs: time.Now().UnixMilli()}
	k.wmMu.Unlock()
}

var errNoBrokers = errString("no kafka brokers")

type errString string

func (e errString) Error() string { return string(e) }
