package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

// codeTTLMinutes is the validity shown in the mail. It mirrors the
// CodeService TTL (10m) for display only; it is not used for verification.
const codeTTLMinutes = 10

var purposeSubject = map[string]string{
	purposeRegister: "注册验证码",
	purposeLogin:    "登录验证码",
	purposeReset:    "重置密码验证码",
	purposeSecurity: "安全验证码",
	purposeAddress:  "提币地址验证码",
	purposeBind:     "绑定验证码",
	purposeWithdraw: "提币验证码",
}

// purposeIntro is the one-line explanation shown above the code so each mail
// reads naturally for its scenario (登录 / 注册 / 重置密码 / 安全验证 ...).
var purposeIntro = map[string]string{
	purposeRegister: "您正在注册 Linbit 账户。请使用下方验证码完成账户创建。",
	purposeLogin:    "您正在登录 Linbit 账户。请使用下方验证码完成本次登录。",
	purposeReset:    "您正在重置 Linbit 账户登录密码。请使用下方验证码完成密码重置。",
	purposeSecurity: "您正在进行安全验证。请使用下方验证码完成本次操作。",
	purposeAddress:  "您正在验证提币地址。请使用下方验证码完成地址验证。",
	purposeBind:     "您正在绑定账户信息。请使用下方验证码完成绑定。",
	purposeWithdraw: "您正在申请提币。请使用下方验证码确认本次提币操作。",
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
	intro := purposeIntro[purpose]
	if intro == "" {
		intro = "请使用下方验证码完成本次操作。"
	}
	text := fmt.Sprintf("%s\n\n验证码：%s\n\n验证码 %d 分钟内有效，请勿泄露给任何人。\n如非本人操作，请忽略本邮件，您的账户仍然安全。\n\n— Linbit 团队", intro, code, codeTTLMinutes)
	htmlBody := codeEmailHTML(title, intro, code, codeTTLMinutes)
	if err := s.sendEmailRaw(ctx, email, codeEmailSubject(title, code), text, htmlBody); err != nil {
		return err
	}
	s.rec.record(email, purpose, code)
	return nil
}

// codeEmailHTML renders a self-contained, inline-styled verification email.
// No external images or <style> blocks, so it survives Gmail/Outlook/Apple
// Mail stripping and renders consistently in light and dark mode.
func codeEmailHTML(title, intro, code string, ttlMin int) string {
	const tpl = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="supported-color-schemes" content="light dark">
<title>{{TITLE}}</title>
</head>
<body style="margin:0;padding:0;background-color:#eef1f5;-webkit-text-size-adjust:100%;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background-color:#eef1f5;">
<tr><td align="center" style="padding:24px 12px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:480px;width:100%;background-color:#ffffff;border-radius:14px;border:1px solid #e3e7ee;overflow:hidden;">
<tr><td style="background-color:#0b1f3a;padding:22px 32px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,'PingFang SC','Microsoft YaHei',sans-serif;">
<span style="display:inline-block;font-size:22px;line-height:1;font-weight:700;letter-spacing:1px;color:#ffffff;">Linbit</span>
<span style="display:inline-block;margin-left:10px;font-size:12px;color:#8fa6c9;vertical-align:3px;">安全验证服务</span>
</td></tr>
<tr><td style="padding:32px 32px 8px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,'PingFang SC','Microsoft YaHei',sans-serif;">
<h1 style="margin:0 0 14px;font-size:19px;line-height:1.4;font-weight:600;color:#0b1f3a;">{{TITLE}}</h1>
<p style="margin:0 0 24px;font-size:14px;line-height:1.7;color:#55607a;">{{INTRO}}</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;">
<tr><td align="center" style="padding:22px 16px;background-color:#f3f7ff;border:1px solid #d6e4ff;border-radius:12px;">
<div style="font-size:12px;letter-spacing:1px;color:#8a94a6;margin-bottom:10px;">验证码</div>
<div style="font-size:38px;line-height:1;font-weight:700;letter-spacing:10px;color:#1a56db;font-family:'SFMono-Regular',Menlo,Consolas,'Courier New',monospace;">{{CODE}}</div>
</td></tr>
</table>
<p style="margin:0 0 10px;font-size:13px;line-height:1.6;color:#55607a;">验证码在 <strong style="color:#0b1f3a;">{{TTL}} 分钟</strong>内有效，请尽快完成验证。为保障账户安全，请勿将验证码透露给任何人（包括 Linbit 客服）。</p>
<p style="margin:0 0 4px;font-size:13px;line-height:1.6;color:#55607a;"><strong style="color:#0b1f3a;">如非本人操作，请忽略本邮件</strong>，您的账户仍然安全。</p>
</td></tr>
<tr><td style="padding:20px 32px 26px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,'PingFang SC','Microsoft YaHei',sans-serif;">
<div style="border-top:1px solid #edf0f5;padding-top:18px;">
<p style="margin:0 0 4px;font-size:12px;line-height:1.6;color:#9aa3b2;">此邮件由系统自动发送，请勿直接回复。</p>
<p style="margin:0;font-size:12px;line-height:1.6;color:#9aa3b2;">© Linbit. 保留所有权利。</p>
</div>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`
	return strings.NewReplacer(
		"{{TITLE}}", title,
		"{{INTRO}}", intro,
		"{{CODE}}", code,
		"{{TTL}}", strconv.Itoa(ttlMin),
	).Replace(tpl)
}

func (s *resendSender) SendEmail(ctx context.Context, to, subject, text string) error {
	return s.sendEmailRaw(ctx, to, subject, text, "")
}

func (s *resendSender) sendEmailRaw(ctx context.Context, to, subject, text, htmlBody string) error {
	m := map[string]interface{}{
		"from": s.from, "to": []string{to}, "subject": subject, "text": text,
	}
	if htmlBody != "" {
		m["html"] = htmlBody
	}
	payload, _ := json.Marshal(m)
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
