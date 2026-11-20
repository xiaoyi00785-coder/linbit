package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// SMS provider selector values for SMS_PROVIDER.
const (
	smsProviderLog    = "log"
	smsProviderAliyun = "aliyun"
	smsProviderTwilio = "twilio"
	smsProviderAuto   = "auto"
)

// SMS purposes map to human text (English + Chinese) and, for Aliyun, to a
// template code in the console. Keep this registry small and centralised so
// adding a new purpose is one edit, not two files.
type smsPurposeText struct {
	// zh/en are the fully-rendered SMS bodies for providers like Twilio that
	// accept arbitrary text. {code} is substituted with the verification code.
	zh, en string
}

var smsPurposeTexts = map[string]smsPurposeText{
	purposeRegister: {
		zh: "【Linbit】您的验证码是 {code}，10 分钟内有效。请勿泄露。",
		en: "[Linbit] Your verification code is {code}. Valid for 10 minutes. Do not share.",
	},
	purposeLogin: {
		zh: "【Linbit】您的登录验证码是 {code}，10 分钟内有效。请勿泄露。",
		en: "[Linbit] Your login code is {code}. Valid for 10 minutes. Do not share.",
	},
	purposeReset: {
		zh: "【Linbit】您的重置密码验证码是 {code}，10 分钟内有效。请勿泄露。",
		en: "[Linbit] Your password-reset code is {code}. Valid for 10 minutes. Do not share.",
	},
	purposeBind: {
		zh: "【Linbit】您的绑定验证码是 {code}，10 分钟内有效。请勿泄露。",
		en: "[Linbit] Your binding code is {code}. Valid for 10 minutes. Do not share.",
	},
	purposeAddress: {
		zh: "【Linbit】您的提币地址验证码是 {code}，10 分钟内有效。请勿泄露。",
		en: "[Linbit] Your withdraw-address code is {code}. Valid for 10 minutes. Do not share.",
	},
	purposeWithdraw: {
		zh: "【Linbit】提币验证码 {code}，请仔细核对地址后使用。",
		en: "[Linbit] Withdrawal code {code}. Double-check the address before use.",
	},
}

// renderSMSBody picks a locale-appropriate body for the recipient. Chinese
// mainland numbers (+86) and Hong Kong/Macau/Taiwan get the Chinese template;
// anything else gets the English one. The {code} placeholder is replaced.
func renderSMSBody(phone, code, purpose string) string {
	txt, ok := smsPurposeTexts[purpose]
	if !ok {
		txt = smsPurposeTexts[purposeRegister]
	}
	body := txt.en
	if isChinesePhone(phone) {
		body = txt.zh
	}
	return strings.ReplaceAll(body, "{code}", code)
}

// isChinesePhone reports whether an E.164 number dials into the Chinese
// mainland (+86). We only treat +86 as Chinese because Aliyun templates are
// written for Simplified Chinese audiences; HK/MO/TW get the English text.
func isChinesePhone(phone string) bool {
	return strings.HasPrefix(phone, "+86")
}

// phoneDialCode extracts the country dial code from an E.164 number. Empty if
// the number is malformed. Only the length prefixes we actually route on are
// recognised (1-, 2- and 3-digit codes covering the common cases).
func phoneDialCode(phone string) string {
	s := strings.TrimPrefix(phone, "+")
	if s == "" {
		return ""
	}
	for n := 3; n >= 1; n-- {
		if len(s) <= n {
			continue
		}
		head := s[:n]
		if _, ok := knownDialCodes[head]; ok {
			return head
		}
	}
	if len(s) >= 1 {
		return s[:1]
	}
	return ""
}

// knownDialCodes is a minimal set of prefixes we route on; being exhaustive is
// not necessary — the fallback is "first digit" which Twilio accepts fine.
var knownDialCodes = map[string]struct{}{
	"1":   {}, // US/CA
	"44":  {}, // UK
	"49":  {}, // DE
	"52":  {}, // MX
	"55":  {}, // BR
	"61":  {}, // AU
	"81":  {}, // JP
	"82":  {}, // KR
	"84":  {}, // VN
	"86":  {}, // CN mainland
	"91":  {}, // IN
	"351": {}, // PT
	"852": {}, // HK
	"853": {}, // MO
	"886": {}, // TW
}

// smsRouter picks a concrete SmsSender per call. Mainland-China numbers (+86)
// go through Aliyun; the rest through Twilio. SMS_FORCE_PROVIDER overrides the
// per-call decision (useful for a provider-wide outage). If only one provider
// is configured, everything goes through it.
type smsRouter struct {
	aliyun *aliyunSmsSender
	twilio *twilioSmsSender
	force  string // "", "aliyun", "twilio"
	fallbk SmsSender
}

func (r *smsRouter) SendSMSCode(ctx context.Context, phone, code, purpose string) error {
	s := r.pick(phone)
	if s == nil {
		return &DeliveryError{Channel: channelSMS, Reason: "not_configured", Detail: "no SMS provider routes this number"}
	}
	err := s.SendSMSCode(ctx, phone, code, purpose)
	if err == nil {
		return nil
	}
	if !smsShouldRetry(err) {
		return err
	}
	// One retry for a transient failure (network blip or provider 5xx). The
	// code row is still valid; the recipient hasn't seen it.
	time.Sleep(200 * time.Millisecond)
	return s.SendSMSCode(ctx, phone, code, purpose)
}

func (r *smsRouter) pick(phone string) SmsSender {
	switch r.force {
	case smsProviderAliyun:
		if r.aliyun != nil {
			return r.aliyun
		}
	case smsProviderTwilio:
		if r.twilio != nil {
			return r.twilio
		}
	}
	if isChinesePhone(phone) && r.aliyun != nil {
		return r.aliyun
	}
	if r.twilio != nil {
		return r.twilio
	}
	if r.aliyun != nil {
		return r.aliyun
	}
	return r.fallbk
}

// smsShouldRetry reports whether a DeliveryError is worth one retry (network
// or provider 5xx). Validation errors (bad phone, blacklist, quota) are not.
func smsShouldRetry(err error) bool {
	de, ok := err.(*DeliveryError)
	if !ok {
		return false
	}
	switch de.Reason {
	case "network", "provider_error":
		return true
	}
	return false
}

// newSmsSender builds the configured SmsSender. SMS_PROVIDER picks the mode:
//
//	log    - the dev mock (also used when nothing is configured)
//	aliyun - all numbers go to Aliyun
//	twilio - all numbers go to Twilio
//	auto   - +86 goes to Aliyun, the rest to Twilio (needs both configured)
//
// SMS_FORCE_PROVIDER overrides the per-call routing decision inside "auto".
func newSmsSender(cfg Config, rec *CodeRecorder, now func() time.Time) SmsSender {
	fallback := SmsSender(logSmsSender{rec: rec})
	mode := strings.ToLower(strings.TrimSpace(cfg.SMSProvider))
	if mode == "" || mode == smsProviderLog {
		log.Printf("sms provider: dev mock (SMS_PROVIDER=%q)", cfg.SMSProvider)
		return fallback
	}
	httpClient := &http.Client{Timeout: 10 * time.Second}

	var aliyun *aliyunSmsSender
	if cfg.AliyunSmsAccessKeyID != "" && cfg.AliyunSmsAccessKeySecret != "" && cfg.AliyunSmsSignName != "" && cfg.AliyunSmsTemplateCode != "" {
		aliyun = newAliyunSmsSender(cfg, rec, httpClient, now)
	}
	var twilio *twilioSmsSender
	if cfg.TwilioAccountSID != "" && cfg.TwilioAuthToken != "" && cfg.TwilioFromNumber != "" {
		twilio = newTwilioSmsSender(cfg, rec, httpClient)
	}

	switch mode {
	case smsProviderAliyun:
		if aliyun == nil {
			log.Printf("sms provider: aliyun requested but not fully configured; falling back to mock")
			return fallback
		}
		log.Printf("sms provider: aliyun (sign=%s)", cfg.AliyunSmsSignName)
		return aliyun
	case smsProviderTwilio:
		if twilio == nil {
			log.Printf("sms provider: twilio requested but not fully configured; falling back to mock")
			return fallback
		}
		log.Printf("sms provider: twilio (from=%s)", cfg.TwilioFromNumber)
		return twilio
	case smsProviderAuto:
		if aliyun == nil && twilio == nil {
			log.Printf("sms provider: auto requested but no provider configured; falling back to mock")
			return fallback
		}
		force := strings.ToLower(strings.TrimSpace(cfg.SMSForceProvider))
		log.Printf("sms provider: auto (aliyun=%t twilio=%t force=%q)", aliyun != nil, twilio != nil, force)
		return &smsRouter{aliyun: aliyun, twilio: twilio, force: force, fallbk: fallback}
	}
	log.Printf("sms provider: unknown SMS_PROVIDER=%q; falling back to mock", cfg.SMSProvider)
	return fallback
}

// redactSecret removes a secret from a response body before it ends up in a
// log line. Used by both SMS providers.
func redactSecret(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[redacted]")
}

// logSMSDelivered logs a successful delivery without the code.
func logSMSDelivered(provider, phone, purpose, requestID string) {
	log.Printf("[sms] provider=%s to=%s purpose=%s status=ok request_id=%s", provider, phone, purpose, requestID)
}

// logSMSFailed logs a failed delivery without the code.
func logSMSFailed(provider, phone, purpose string, de *DeliveryError) {
	log.Printf("[sms] provider=%s to=%s purpose=%s status=err reason=%s status_code=%d detail=%s",
		provider, phone, purpose, de.Reason, de.Status, de.Detail)
}

// errUnexpectedSMSStatus is a convenience constructor for an upstream non-2xx.
func errUnexpectedSMSStatus(status int, body string) *DeliveryError {
	return &DeliveryError{
		Channel: channelSMS,
		Status:  status,
		Reason:  fmt.Sprintf("http_%d", status),
		Detail:  body,
	}
}
