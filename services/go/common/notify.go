package common

// notify.go：把站内通知交给账户服务。失败只记日志，不挡住业务。没配 ACCOUNT_INTERNAL_URL 或 INTERNAL_TOKEN 则什么都不做。

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// Notification kinds shown as tabs in the user's message center.
const (
	NoticeSystem   = "system"
	NoticeTrade    = "trade"
	NoticeActivity = "activity"
	NoticeSecurity = "security"
)

// Notice is one user notification sent to the account service, which stores
// it in the in-app inbox and, depending on the user's preferences (security is
// always emailed), also delivers it by email.
type Notice struct {
	UID   string `json:"uid"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// Email forces (true) or suppresses (false) the email channel; nil uses the
	// user's preferences.
	Email *bool `json:"email,omitempty"`
	// DedupKey is an optional end-to-end idempotency key for account's durable
	// inbox. It is required for outbox-based producers such as price alerts.
	DedupKey string `json:"dedupKey,omitempty"`
}

var notifyClient = &http.Client{Timeout: 5 * time.Second}

// Notify 在后台把通知交给账户服务。失败只记日志，不让业务失败。
// 读 ACCOUNT_INTERNAL_URL 和 INTERNAL_TOKEN；缺了就是空操作。
func Notify(n Notice) {
	base := os.Getenv("ACCOUNT_INTERNAL_URL")
	tok := os.Getenv("INTERNAL_TOKEN")
	if base == "" || tok == "" || n.UID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		b, _ := json.Marshal(n)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/internal/notifications", bytes.NewReader(b))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", tok)
		resp, err := notifyClient.Do(req)
		if err != nil {
			log.Printf("notify %s %q: %v", n.Kind, n.Title, err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Printf("notify %s %q: status %d", n.Kind, n.Title, resp.StatusCode)
		}
	}()
}
