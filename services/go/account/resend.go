package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// resendSender delivers email through the Resend HTTP API
// (POST /emails, Bearer API key). The key is never logged.
type resendSender struct {
	url, key, from string
	rec            *CodeRecorder // dev only; nil in production
	http           *http.Client
}

func newResendSender(url, key, from string, rec *CodeRecorder) *resendSender {
	return &resendSender{url: url, key: key, from: from, rec: rec, http: &http.Client{Timeout: 10 * time.Second}}
}

var purposeSubject = map[string]string{
	purposeRegister: "注册验证码",
	purposeLogin:    "登录验证码",
	purposeReset:    "重置密码验证码",
	purposeAddress:  "提币地址验证码",
	purposeBind:     "绑定验证码",
	purposeWithdraw: "提币验证码",
}

// codeEmailSubject puts the code in the subject so each mail is distinct
// (Gmail otherwise threads identical subjects and hides new codes), e.g.
// "Linbit 登录验证码 123456".
func codeEmailSubject(title, code string) string {
	return "Linbit " + title + " " + code
}

func (s *resendSender) SendEmailCode(ctx context.Context, email, code, purpose string) error {
	title := purposeSubject[purpose]
	if title == "" {
		title = "验证码"
	}
	body := fmt.Sprintf("您的%s为：%s\n\n10 分钟内有效，请勿泄露给任何人。如非本人操作请忽略本邮件。", title, code)
	if err := s.SendEmail(ctx, email, codeEmailSubject(title, code), body); err != nil {
		return err
	}
	s.rec.record(email, purpose, code)
	return nil
}

func (s *resendSender) SendEmail(ctx context.Context, to, subject, text string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"from": s.from, "to": []string{to}, "subject": subject, "text": text,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Content-Type", "application/json")
	if s.key == "" || s.from == "" {
		return &DeliveryError{Channel: channelEmail, Reason: "not_configured", Detail: "RESEND_API_KEY or RESEND_FROM empty"}
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return &DeliveryError{Channel: channelEmail, Reason: "network", Detail: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &DeliveryError{Channel: channelEmail, Status: resp.StatusCode, Reason: resendReason(resp.StatusCode, b), Detail: redactKey(string(b), s.key)}
	}
	return nil
}

// resendReason maps a Resend error response to a short ops-facing reason.
// See https://resend.com/docs/api-reference/errors.
func resendReason(status int, body []byte) string {
	var e struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	msg := strings.ToLower(e.Name + " " + e.Message)
	if e.Name == "" && e.Message == "" {
		msg = strings.ToLower(string(body))
	}
	switch {
	case status == 401 || strings.Contains(msg, "missing_api_key") || strings.Contains(msg, "invalid_api_key") || strings.Contains(msg, "api key is invalid"):
		return "invalid_api_key"
	case status/100 == 4 && status != 401 && status != 403 && status != 429 && isRecipientError(msg):
		return "invalid_recipient"
	case strings.Contains(msg, "domain") && (strings.Contains(msg, "verif") || strings.Contains(msg, "not")):
		return "from_domain_not_verified"
	case strings.Contains(msg, "testing emails") || strings.Contains(msg, "own email"):
		return "sandbox_recipient_only"
	case strings.Contains(msg, "restricted_api_key") || strings.Contains(msg, "restricted"):
		return "restricted_api_key"
	case status == 403:
		return "forbidden"
	case status == 422:
		if strings.Contains(msg, "from") {
			return "invalid_from"
		}
		return "invalid_request"
	case status == 429:
		return "provider_rate_limited"
	case status >= 500:
		return "provider_error"
	}
	return fmt.Sprintf("http_%d", status)
}

// isRecipientError reports whether a Resend 4xx message blames the "to"
// address (bad syntax, example.com / reserved domain, undeliverable).
func isRecipientError(msg string) bool {
	if strings.Contains(msg, "from") && !strings.Contains(msg, "`to`") && !strings.Contains(msg, " to ") {
		return false
	}
	for _, k := range []string{"`to`", "to field", "invalid_to", "recipient", "invalid email address", "email address", "example.com", "undeliverable", "not deliverable"} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

func redactKey(s, key string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "[redacted]")
	}
	return s
}
