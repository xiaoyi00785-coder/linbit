package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// attachPaperSnapshots loads private paper channel snapshots from the paper
// service. When paperURL is empty, subscribe still requires login and sends
// no snapshot rather than an invented balance.
func attachPaperSnapshots(h *Hub, paperURL, token string) {
	paperURL = strings.TrimRight(strings.TrimSpace(paperURL), "/")
	if h == nil || paperURL == "" || token == "" {
		return
	}
	client := &http.Client{Timeout: 2 * time.Second}
	h.SetPaperSnapshot(func(uid, channel string) any {
		req, err := http.NewRequest(http.MethodGet, paperURL+"/internal/paper/futures/snapshot?userId="+url.QueryEscape(uid), nil)
		if err != nil {
			return nil
		}
		req.Header.Set("X-Internal-Token", token)
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil
		}
		var body map[string]any
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil {
			return nil
		}
		body["channel"] = channel
		return body
	})
}
