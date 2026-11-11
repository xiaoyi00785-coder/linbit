package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// attachPaperSnapshots 向 paper 服务要私有模拟盘频道的快照。
// paperURL 为空时，订阅仍要求登录，并且不发快照，也不编造余额。
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
