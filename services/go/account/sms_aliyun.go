package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// aliyunSmsSender delivers SMS via Aliyun DYSMSAPI (POP). It signs each
// request with HMAC-SHA1 over a canonical query string per
// https://help.aliyun.com/document_detail/55284.html — AccessKeyId stays in
// the query, the secret never leaves the process, nothing is logged in the
// clear.
type aliyunSmsSender struct {
	endpoint       string // e.g. https://dysmsapi.aliyuncs.com/
	accessKeyID    string
	accessSecret   string
	signName       string
	defaultTplCode string
	purposeTplCode map[string]string // per-purpose template code override
	regionID       string
	rec            *CodeRecorder
	http           *http.Client
	now            func() time.Time
	nonce          func() string
}

func newAliyunSmsSender(cfg Config, rec *CodeRecorder, client *http.Client, now func() time.Time) *aliyunSmsSender {
	endpoint := cfg.AliyunSmsEndpoint
	if endpoint == "" {
		endpoint = "https://dysmsapi.aliyuncs.com/"
	}
	region := cfg.AliyunSmsRegionID
	if region == "" {
		region = "cn-hangzhou"
	}
	return &aliyunSmsSender{
		endpoint:       endpoint,
		accessKeyID:    cfg.AliyunSmsAccessKeyID,
		accessSecret:   cfg.AliyunSmsAccessKeySecret,
		signName:       cfg.AliyunSmsSignName,
		defaultTplCode: cfg.AliyunSmsTemplateCode,
		purposeTplCode: aliyunPurposeTemplates(cfg),
		regionID:       region,
		rec:            rec,
		http:           client,
		now:            now,
		nonce:          defaultAliyunNonce,
	}
}

// aliyunPurposeTemplates maps each code purpose to its Aliyun template code.
// Each falls back to the default template when its own is unset.
func aliyunPurposeTemplates(cfg Config) map[string]string {
	m := map[string]string{}
	if cfg.AliyunSmsTemplateCodeWithdraw != "" {
		m[purposeWithdraw] = cfg.AliyunSmsTemplateCodeWithdraw
	}
	if cfg.AliyunSmsTemplateCodeLogin != "" {
		m[purposeLogin] = cfg.AliyunSmsTemplateCodeLogin
	}
	if cfg.AliyunSmsTemplateCodeReset != "" {
		m[purposeReset] = cfg.AliyunSmsTemplateCodeReset
	}
	return m
}

// SendSMSCode dispatches a verification code. Aliyun-registered templates
// carry the full text; we only pass ${code} as a TemplateParam.
func (s *aliyunSmsSender) SendSMSCode(ctx context.Context, phone, code, purpose string) error {
	if s.accessKeyID == "" || s.accessSecret == "" || s.signName == "" || s.defaultTplCode == "" {
		return &DeliveryError{Channel: channelSMS, Reason: "not_configured", Detail: "ALIYUN_SMS_* env vars incomplete"}
	}
	tpl := s.defaultTplCode
	if override, ok := s.purposeTplCode[purpose]; ok {
		tpl = override
	}
	// Aliyun expects the national number for +86 (no country code), but
	// international numbers must keep the country code (no leading +).
	national := aliyunPhoneFormat(phone)
	param, _ := json.Marshal(map[string]string{"code": code})

	params := map[string]string{
		"AccessKeyId":      s.accessKeyID,
		"Action":           "SendSms",
		"Format":           "JSON",
		"PhoneNumbers":     national,
		"RegionId":         s.regionID,
		"SignName":         s.signName,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   s.nonce(),
		"SignatureVersion": "1.0",
		"TemplateCode":     tpl,
		"TemplateParam":    string(param),
		"Timestamp":        s.now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          "2017-05-25",
	}
	signed := aliyunSign(http.MethodGet, params, s.accessSecret)

	u, err := url.Parse(s.endpoint)
	if err != nil {
		return &DeliveryError{Channel: channelSMS, Reason: "config", Detail: err.Error()}
	}
	u.RawQuery = signed
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return &DeliveryError{Channel: channelSMS, Reason: "config", Detail: err.Error()}
	}
	resp, err := s.http.Do(req)
	if err != nil {
		de := &DeliveryError{Channel: channelSMS, Reason: "network", Detail: err.Error()}
		logSMSFailed("aliyun", phone, purpose, de)
		return de
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	// Aliyun returns 200 + JSON whose Code field is "OK" on success and a
	// specific error code (e.g. isv.MOBILE_NUMBER_ILLEGAL, isv.BUSINESS_LIMIT_CONTROL,
	// isv.MOBILE_IN_BLACK) otherwise. The HTTP status reflects transport, not
	// business validity, so we read Code first.
	var ar aliyunSMSResponse
	_ = json.Unmarshal(body, &ar)
	ar.Message = redactSecret(ar.Message, s.accessSecret)

	if resp.StatusCode/100 != 2 {
		de := errUnexpectedSMSStatus(resp.StatusCode, redactSecret(string(body), s.accessSecret))
		if resp.StatusCode >= 500 {
			de.Reason = "provider_error"
		}
		logSMSFailed("aliyun", phone, purpose, de)
		return de
	}
	if ar.Code != "OK" {
		de := &DeliveryError{
			Channel: channelSMS,
			Status:  resp.StatusCode,
			Reason:  aliyunReason(ar.Code),
			Detail:  fmt.Sprintf("code=%s message=%s request_id=%s", ar.Code, ar.Message, ar.RequestID),
		}
		logSMSFailed("aliyun", phone, purpose, de)
		return de
	}
	logSMSDelivered("aliyun", phone, purpose, ar.RequestID)
	s.rec.record(phone, purpose, code)
	return nil
}

type aliyunSMSResponse struct {
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	RequestID string `json:"RequestId"`
	BizID     string `json:"BizId"`
}

// aliyunReason maps a documented SendSms error code to a short, stable reason
// label. Anything unrecognised is returned as-is so ops can grep for it.
func aliyunReason(code string) string {
	switch code {
	case "isv.MOBILE_NUMBER_ILLEGAL", "isv.PHONE_NUMBER_ILLEGAL":
		return "invalid_recipient"
	case "isv.MOBILE_COUNT_OVER_LIMIT":
		return "too_many_recipients"
	case "isv.BUSINESS_LIMIT_CONTROL", "isv.DAY_LIMIT_CONTROL", "isv.MONTH_LIMIT_CONTROL":
		return "provider_rate_limited"
	case "isv.OUT_OF_SERVICE", "isv.ACCOUNT_NOT_ENOUGH":
		return "out_of_service"
	case "isv.PRODUCT_UN_SUBSCRIPT", "isv.PRODUCT_UNSUBSCRIBE":
		return "not_subscribed"
	case "isv.TEMPLATE_MISSING_PARAMETERS", "isv.INVALID_PARAMETERS":
		return "invalid_request"
	case "isv.SIGN_NAME_ILLEGAL", "isv.SMS_TEMPLATE_ILLEGAL":
		return "invalid_config"
	case "isv.MOBILE_IN_BLACK":
		return "blacklisted_recipient"
	case "InvalidAccessKeyId.NotFound", "SignatureDoesNotMatch", "IncompleteSignature":
		return "invalid_api_key"
	case "QuotaExceeded":
		return "quota_exceeded"
	case "":
		return "unknown"
	}
	// Server-side 5xx translated to a 200+Code by POP.
	if strings.HasPrefix(code, "isp.") || strings.HasPrefix(code, "Throttling") {
		return "provider_error"
	}
	return code
}

// aliyunPhoneFormat renders an E.164 number the way DYSMSAPI accepts it.
// Mainland +86 numbers are passed as 11-digit national numbers; everything
// else keeps the country code (without the leading +).
func aliyunPhoneFormat(phone string) string {
	if strings.HasPrefix(phone, "+86") {
		return strings.TrimPrefix(phone, "+86")
	}
	return strings.TrimPrefix(phone, "+")
}

// aliyunSign builds the signed query string per the POP protocol. Returns the
// canonical query string with the Signature parameter appended.
func aliyunSign(method string, params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for i, k := range keys {
		if i > 0 {
			canonical.WriteByte('&')
		}
		canonical.WriteString(aliyunPercentEncode(k))
		canonical.WriteByte('=')
		canonical.WriteString(aliyunPercentEncode(params[k]))
	}
	stringToSign := method + "&" + aliyunPercentEncode("/") + "&" + aliyunPercentEncode(canonical.String())

	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return canonical.String() + "&Signature=" + aliyunPercentEncode(sig)
}

// aliyunPercentEncode follows RFC 3986 the way the POP SDK does: Go's default
// url.QueryEscape encodes space as '+' and leaves '*' unescaped, both of
// which the POP server rejects.
func aliyunPercentEncode(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	e = strings.ReplaceAll(e, "*", "%2A")
	e = strings.ReplaceAll(e, "%7E", "~")
	return e
}

var aliyunNonceRNG = rand.New(rand.NewSource(time.Now().UnixNano()))

func defaultAliyunNonce() string {
	b := make([]byte, 16)
	_, _ = aliyunNonceRNG.Read(b)
	return hex.EncodeToString(b)
}
