package common

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// telegram.go: operator alerts via the Telegram Bot API. TELEGRAM_BOT_TOKEN and
// TELEGRAM_CHAT_ID come from the environment; TELEGRAM_API_URL (default
// https://api.telegram.org) can point at a local fake for tests. The token is
// never logged. Without a token or chat id the alert is only logged.

var telegramClient = &http.Client{Timeout: 8 * time.Second}

// TelegramConfigured reports whether Telegram alerts will actually be sent.
func TelegramConfigured() bool {
	return strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")) != "" && strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")) != ""
}

// SendTelegram sends text to the configured chat and returns an error when the
// API call fails. It is synchronous; use AlertTelegram for fire-and-forget.
func SendTelegram(ctx context.Context, text string) error {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	chat := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	if token == "" || chat == "" {
		log.Printf("telegram not configured; alert: %s", text)
		return fmt.Errorf("telegram not configured")
	}
	base := strings.TrimRight(Env("TELEGRAM_API_URL", "https://api.telegram.org"), "/")
	body, _ := json.Marshal(map[string]any{"chat_id": chat, "text": text, "disable_web_page_preview": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram request build failed")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := telegramClient.Do(req)
	if err != nil {
		// Never include err: *url.Error embeds the URL, which contains the token.
		return fmt.Errorf("telegram send failed (network)")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("telegram send failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

// AlertTelegram sends text in the background; failures are logged (without the
// token) and never block the caller.
func AlertTelegram(text string) {
	log.Printf("ALERT: %s", text)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := SendTelegram(ctx, text); err != nil && TelegramConfigured() {
			log.Printf("telegram alert: %v", err)
		}
	}()
}

// TestnetSeedAllowed reports whether the virtual market-maker seed
// (mm_testnet_seed) may be used: never in production, only with
// WALLET_CHAIN=mock|testnet (default mock). The reason explains a refusal.
func TestnetSeedAllowed() (bool, string) {
	if IsProduction() {
		return false, "LINBIT_ENV=production"
	}
	switch strings.ToLower(strings.TrimSpace(Env("WALLET_CHAIN", "mock"))) {
	case "mock", "testnet":
		return true, ""
	default:
		return false, "WALLET_CHAIN is not mock/testnet"
	}
}
