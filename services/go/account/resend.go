package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func (s *resendSender) SendEmailCode(ctx context.Context, email, code, purpose string) error {
	title := purposeSubject[purpose]
	if title == "" {
		title = "验证码"
	}
	body := fmt.Sprintf("您的%s为：%s\n\n10 分钟内有效，请勿泄露给任何人。如非本人操作请忽略本邮件。", title, code)
	if err := s.SendEmail(ctx, email, "[Linbit] "+title, body); err != nil {
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
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend: status %d: %s", resp.StatusCode, b)
	}
	return nil
}
