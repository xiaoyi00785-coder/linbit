package common

// telegram.go：运维告警，走 Telegram Bot API。
// TELEGRAM_BOT_TOKEN、TELEGRAM_CHAT_ID 来自环境变量。TELEGRAM_API_URL 默认 https://api.telegram.org，测试可指到本地。
// 令牌不进日志。没配 token 或 chat id 时只打日志。

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

// TestnetSeedAllowed 判断能否做虚拟做市注资 mm_testnet_seed。看的是链，不是 LINBIT_ENV：
// WALLET_CHAIN=testnet 即使 LINBIT_ENV=production 也允许（测试网机器是加固的）；
// WALLET_CHAIN=mock 只在非生产；mainnet 或未知一律拒绝。原因里不含密钥。
func TestnetSeedAllowed() (bool, string) {
	if IsMainnet() {
		return false, "NETWORK=mainnet: virtual seed is forbidden"
	}
	switch c := strings.ToLower(strings.TrimSpace(Env("WALLET_CHAIN", "mock"))); c {
	case "testnet":
		return true, "WALLET_CHAIN=testnet (virtual, unbacked)"
	case "mock":
		if IsProduction() {
			return false, "WALLET_CHAIN=mock with LINBIT_ENV=production"
		}
		return true, "WALLET_CHAIN=mock"
	default:
		return false, "WALLET_CHAIN=" + c + " is not testnet: virtual seed is forbidden on mainnet"
	}
}
