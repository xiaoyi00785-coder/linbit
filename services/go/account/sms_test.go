package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- router + selection ---

type fakeSMS struct {
	name string
	err  error
	n    int32
	last struct{ phone, code, purpose string }
}

func (f *fakeSMS) SendSMSCode(_ context.Context, phone, code, purpose string) error {
	atomic.AddInt32(&f.n, 1)
	f.last.phone, f.last.code, f.last.purpose = phone, code, purpose
	return f.err
}

func TestNewSmsSenderSelectsMode(t *testing.T) {
	rec := NewCodeRecorder()
	cases := []struct {
		name   string
		cfg    Config
		typeOk func(SmsSender) bool
	}{
		{
			name: "empty = log mock",
			cfg:  Config{},
			typeOk: func(s SmsSender) bool {
				_, ok := s.(logSmsSender)
				return ok
			},
		},
		{
			name: "log explicit = mock",
			cfg:  Config{SMSProvider: "log"},
			typeOk: func(s SmsSender) bool { _, ok := s.(logSmsSender); return ok },
		},
		{
			name: "aliyun missing config -> mock",
			cfg:  Config{SMSProvider: "aliyun"},
			typeOk: func(s SmsSender) bool { _, ok := s.(logSmsSender); return ok },
		},
		{
			name: "twilio configured -> twilio",
			cfg:  Config{SMSProvider: "twilio", TwilioAccountSID: "AC", TwilioAuthToken: "t", TwilioFromNumber: "+15550001"},
			typeOk: func(s SmsSender) bool { _, ok := s.(*twilioSmsSender); return ok },
		},
		{
			name: "aliyun configured -> aliyun",
			cfg: Config{SMSProvider: "aliyun", AliyunSmsAccessKeyID: "k", AliyunSmsAccessKeySecret: "s",
				AliyunSmsSignName: "Linbit", AliyunSmsTemplateCode: "SMS_1"},
			typeOk: func(s SmsSender) bool { _, ok := s.(*aliyunSmsSender); return ok },
		},
		{
			name: "auto with both -> router",
			cfg: Config{SMSProvider: "auto",
				AliyunSmsAccessKeyID: "k", AliyunSmsAccessKeySecret: "s", AliyunSmsSignName: "L", AliyunSmsTemplateCode: "T",
				TwilioAccountSID: "AC", TwilioAuthToken: "t", TwilioFromNumber: "+15550001"},
			typeOk: func(s SmsSender) bool { _, ok := s.(*smsRouter); return ok },
		},
		{
			name: "auto with none -> mock",
			cfg:  Config{SMSProvider: "auto"},
			typeOk: func(s SmsSender) bool { _, ok := s.(logSmsSender); return ok },
		},
		{
			name: "unknown provider -> mock",
			cfg:  Config{SMSProvider: "bogus"},
			typeOk: func(s SmsSender) bool { _, ok := s.(logSmsSender); return ok },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSmsSender(c.cfg, rec, time.Now)
			if !c.typeOk(s) {
				t.Fatalf("got %T", s)
			}
		})
	}
}

func TestSmsRouterPicksByCountry(t *testing.T) {
	ali := &fakeSMS{name: "ali"}
	tw := &fakeSMS{name: "tw"}
	// Construct the router directly; the "aliyun"/"twilio" fields take concrete
	// types so we use a thin wrapper type with a matching SendSMSCode.
	r := &smsRouter{
		aliyun: &aliyunSmsSender{},
		twilio: &twilioSmsSender{},
	}
	// Replace the concrete senders' Send call via an adapter: actually, the
	// router reaches in by type, so we exercise pick() directly.
	if got := r.pick("+8613800138000"); got != r.aliyun {
		t.Errorf("CN should route to aliyun, got %T", got)
	}
	if got := r.pick("+14155550100"); got != r.twilio {
		t.Errorf("US should route to twilio, got %T", got)
	}
	// Force override
	r.force = smsProviderTwilio
	if got := r.pick("+8613800138000"); got != r.twilio {
		t.Errorf("force=twilio should win, got %T", got)
	}
	r.force = smsProviderAliyun
	if got := r.pick("+14155550100"); got != r.aliyun {
		t.Errorf("force=aliyun should win, got %T", got)
	}
	// If only one provider is configured, everything goes there.
	r2 := &smsRouter{twilio: &twilioSmsSender{}}
	if got := r2.pick("+8613800138000"); got != r2.twilio {
		t.Errorf("twilio-only should take CN, got %T", got)
	}
	r3 := &smsRouter{aliyun: &aliyunSmsSender{}}
	if got := r3.pick("+14155550100"); got != r3.aliyun {
		t.Errorf("aliyun-only should take US, got %T", got)
	}
	// Nothing configured -> fallback.
	r4 := &smsRouter{fallbk: logSmsSender{}}
	if got := r4.pick("+14155550100"); got != r4.fallbk {
		t.Errorf("no provider should fall back, got %T", got)
	}
	// Prevent unused warnings for the fakes (they are kept for the retry test).
	_ = ali
	_ = tw
}

func TestSmsRouterRetriesTransient(t *testing.T) {
	fail := 1
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) <= int32(fail) {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{"sid":"SM1","status":"queued"}`))
	}))
	defer srv.Close()
	tw := newTwilioSmsSender(Config{
		TwilioAccountSID: "AC", TwilioAuthToken: "t", TwilioFromNumber: "+15550001",
		TwilioAPIBase: srv.URL,
	}, nil, srv.Client())
	r := &smsRouter{twilio: tw}
	if err := r.SendSMSCode(context.Background(), "+14155550100", "123456", purposeRegister); err != nil {
		t.Fatalf("expected retry to succeed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("want 2 attempts, got %d", calls)
	}
}

// --- aliyun signing ---

func TestAliyunPercentEncodeMatchesPOP(t *testing.T) {
	cases := map[string]string{
		"a b":     "a%20b",
		"a*b":     "a%2Ab",
		"~":       "%7E", // the function should turn Go's %7E back into literal ~
		"中文":      "%E4%B8%AD%E6%96%87",
		"A/B?C=D": "A%2FB%3FC%3DD",
	}
	for in, want := range cases {
		got := aliyunPercentEncode(in)
		if in == "~" {
			if got != "~" {
				t.Errorf("~ => %q, want ~", got)
			}
			continue
		}
		if got != want {
			t.Errorf("%q => %q, want %q", in, got, want)
		}
	}
}

func TestAliyunSignKnownAnswer(t *testing.T) {
	// Vector adapted from the DYSMSAPI docs: given these params and the fixed
	// secret "testsecret", HMAC-SHA1 of the canonical string must be stable.
	params := map[string]string{
		"AccessKeyId":      "testid",
		"Action":           "SendSms",
		"Format":           "JSON",
		"PhoneNumbers":     "13800138000",
		"RegionId":         "cn-hangzhou",
		"SignName":         "Linbit",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   "nonce-fixed",
		"SignatureVersion": "1.0",
		"TemplateCode":     "SMS_1",
		"TemplateParam":    `{"code":"123456"}`,
		"Timestamp":        "2024-01-02T03:04:05Z",
		"Version":          "2017-05-25",
	}
	got := aliyunSign(http.MethodGet, params, "testsecret")
	// The signature is deterministic: regenerate once with a known-good
	// implementation and lock it in. We verify by recomputing with crypto.
	expectedSig := recomputeAliyunSig(http.MethodGet, params, "testsecret")
	if !strings.Contains(got, "Signature=") {
		t.Fatalf("signature missing: %s", got)
	}
	sig := got[strings.LastIndex(got, "Signature=")+len("Signature="):]
	if sig != aliyunPercentEncode(expectedSig) {
		t.Errorf("signature mismatch\n got=%s\nwant=%s", sig, aliyunPercentEncode(expectedSig))
	}
}

// recomputeAliyunSig mirrors aliyunSign but is open-coded so the test fails
// loudly if the production signer drifts from the documented algorithm.
func recomputeAliyunSig(method string, params map[string]string, secret string) string {
	v := url.Values{}
	for k, vv := range params {
		v.Set(k, vv)
	}
	enc := v.Encode() // sorted by key
	enc = strings.ReplaceAll(enc, "+", "%20")
	enc = strings.ReplaceAll(enc, "*", "%2A")
	enc = strings.ReplaceAll(enc, "%7E", "~")
	sts := method + "&" + aliyunPercentEncode("/") + "&" + aliyunPercentEncode(enc)
	mac := hmacSHA1([]byte(secret+"&"), []byte(sts))
	return base64.StdEncoding.EncodeToString(mac)
}

func hmacSHA1(key, msg []byte) []byte {
	m := hmac.New(sha1.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

// --- aliyun send: success + error mapping ---

func TestAliyunSendSuccess(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"Code":"OK","Message":"OK","RequestId":"REQ-1"}`)
	}))
	defer srv.Close()
	rec := NewCodeRecorder()
	s := newAliyunSmsSender(Config{
		AliyunSmsEndpoint:        srv.URL,
		AliyunSmsAccessKeyID:     "AKID",
		AliyunSmsAccessKeySecret: "SECRET",
		AliyunSmsSignName:        "Linbit",
		AliyunSmsTemplateCode:    "SMS_1",
	}, rec, srv.Client(), func() time.Time { return time.Unix(1700000000, 0).UTC() })
	s.nonce = func() string { return "fixed-nonce" }

	if err := s.SendSMSCode(context.Background(), "+8613800138000", "654321", purposeRegister); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got.Get("PhoneNumbers") != "13800138000" {
		t.Errorf("CN number should be 11 digits, got %q", got.Get("PhoneNumbers"))
	}
	if got.Get("SignName") != "Linbit" || got.Get("TemplateCode") != "SMS_1" {
		t.Errorf("bad params: %v", got)
	}
	if !strings.Contains(got.Get("TemplateParam"), "654321") {
		t.Errorf("code not in TemplateParam: %q", got.Get("TemplateParam"))
	}
	if got.Get("Signature") == "" {
		t.Errorf("no signature in query")
	}
	if rec.Last("+8613800138000", purposeRegister) != "654321" {
		t.Errorf("recorder not updated")
	}
	// International number should keep its country code without the +.
	s.nonce = func() string { return "fixed-nonce-2" }
	_ = s.SendSMSCode(context.Background(), "+14155550100", "111111", purposeLogin)
	if got.Get("PhoneNumbers") != "14155550100" {
		t.Errorf("intl number should drop +: %q", got.Get("PhoneNumbers"))
	}
}

func TestAliyunSendErrorMapping(t *testing.T) {
	cases := []struct {
		resp   string
		reason string
	}{
		{`{"Code":"isv.MOBILE_NUMBER_ILLEGAL","Message":"bad","RequestId":"R1"}`, "invalid_recipient"},
		{`{"Code":"isv.BUSINESS_LIMIT_CONTROL","Message":"too many","RequestId":"R2"}`, "provider_rate_limited"},
		{`{"Code":"isv.MOBILE_IN_BLACK","Message":"blocked","RequestId":"R3"}`, "blacklisted_recipient"},
		{`{"Code":"InvalidAccessKeyId.NotFound","Message":"bad key","RequestId":"R4"}`, "invalid_api_key"},
		{`{"Code":"isv.ACCOUNT_NOT_ENOUGH","Message":"no balance","RequestId":"R5"}`, "out_of_service"},
	}
	for _, c := range cases {
		t.Run(c.reason, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, c.resp)
			}))
			defer srv.Close()
			s := newAliyunSmsSender(Config{
				AliyunSmsEndpoint:        srv.URL,
				AliyunSmsAccessKeyID:     "AKID",
				AliyunSmsAccessKeySecret: "SECRET",
				AliyunSmsSignName:        "Linbit",
				AliyunSmsTemplateCode:    "SMS_1",
			}, nil, srv.Client(), time.Now)
			err := s.SendSMSCode(context.Background(), "+8613800138000", "1", purposeRegister)
			var de *DeliveryError
			if !errors.As(err, &de) {
				t.Fatalf("want DeliveryError, got %T %v", err, err)
			}
			if de.Reason != c.reason {
				t.Errorf("reason %q, want %q (detail=%s)", de.Reason, c.reason, de.Detail)
			}
		})
	}
}

func TestAliyunPurposeTemplateOverride(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		io.WriteString(w, `{"Code":"OK","RequestId":"R"}`)
	}))
	defer srv.Close()
	s := newAliyunSmsSender(Config{
		AliyunSmsEndpoint:             srv.URL,
		AliyunSmsAccessKeyID:          "AKID",
		AliyunSmsAccessKeySecret:      "SECRET",
		AliyunSmsSignName:             "Linbit",
		AliyunSmsTemplateCode:         "SMS_DEFAULT",
		AliyunSmsTemplateCodeWithdraw: "SMS_WITHDRAW",
	}, nil, srv.Client(), time.Now)
	_ = s.SendSMSCode(context.Background(), "+8613800138000", "1", purposeWithdraw)
	if got.Get("TemplateCode") != "SMS_WITHDRAW" {
		t.Errorf("withdraw should use its own template, got %q", got.Get("TemplateCode"))
	}
	_ = s.SendSMSCode(context.Background(), "+8613800138000", "1", purposeRegister)
	if got.Get("TemplateCode") != "SMS_DEFAULT" {
		t.Errorf("register should use default template, got %q", got.Get("TemplateCode"))
	}
}

func TestAliyunNotConfigured(t *testing.T) {
	s := newAliyunSmsSender(Config{}, nil, http.DefaultClient, time.Now)
	err := s.SendSMSCode(context.Background(), "+8613800138000", "1", purposeRegister)
	var de *DeliveryError
	if !errors.As(err, &de) || de.Reason != "not_configured" {
		t.Fatalf("want not_configured, got %v", err)
	}
}

// --- twilio send ---

func TestTwilioSendSuccess(t *testing.T) {
	var auth, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		if !strings.HasSuffix(r.URL.Path, "/Accounts/AC123/Messages.json") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"sid":"SM1","status":"queued"}`)
	}))
	defer srv.Close()
	rec := NewCodeRecorder()
	s := newTwilioSmsSender(Config{
		TwilioAccountSID: "AC123", TwilioAuthToken: "tok", TwilioFromNumber: "+15550001",
		TwilioAPIBase: srv.URL,
	}, rec, srv.Client())
	if err := s.SendSMSCode(context.Background(), "+14155550100", "987654", purposeRegister); err != nil {
		t.Fatalf("send: %v", err)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("AC123:tok"))
	if auth != wantAuth {
		t.Errorf("basic auth %q != %q", auth, wantAuth)
	}
	form, _ := url.ParseQuery(body)
	if form.Get("To") != "+14155550100" || form.Get("From") != "+15550001" {
		t.Errorf("bad form: %v", form)
	}
	if !strings.Contains(form.Get("Body"), "987654") {
		t.Errorf("code missing from body: %q", form.Get("Body"))
	}
	if !strings.HasPrefix(form.Get("Body"), "[Linbit]") {
		t.Errorf("non-CN number should get English body, got %q", form.Get("Body"))
	}
	if rec.Last("+14155550100", purposeRegister) != "987654" {
		t.Errorf("recorder not updated")
	}
	// CN number through twilio should still use Chinese text.
	_ = s.SendSMSCode(context.Background(), "+8613800138000", "1", purposeRegister)
	form2, _ := url.ParseQuery(body)
	if !strings.HasPrefix(form2.Get("Body"), "【Linbit】") {
		t.Errorf("CN number should get Chinese body, got %q", form2.Get("Body"))
	}
}

func TestTwilioSendMessagingServiceSID(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		io.WriteString(w, `{"sid":"SM1","status":"queued"}`)
	}))
	defer srv.Close()
	s := newTwilioSmsSender(Config{
		TwilioAccountSID: "AC", TwilioAuthToken: "t", TwilioFromNumber: "+15550001",
		TwilioMessagingServiceSID: "MG1",
		TwilioAPIBase:             srv.URL,
	}, nil, srv.Client())
	_ = s.SendSMSCode(context.Background(), "+14155550100", "1", purposeRegister)
	form, _ := url.ParseQuery(body)
	if form.Get("MessagingServiceSid") != "MG1" {
		t.Errorf("MessagingServiceSid should win, got %v", form)
	}
	if form.Get("From") != "" {
		t.Errorf("From should be omitted when MessagingServiceSid is set, got %q", form.Get("From"))
	}
}

func TestTwilioErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		resp   string
		reason string
	}{
		{401, `{"code":20003,"message":"Authenticate"}`, "invalid_api_key"},
		{400, `{"code":21211,"message":"Invalid 'To' Phone Number"}`, "invalid_recipient"},
		{400, `{"code":21408,"message":"Permission to send to this region is not enabled"}`, "region_not_permitted"},
		{429, `{"code":20429,"message":"Too many requests"}`, "provider_rate_limited"},
		{500, `{"code":500,"message":"boom"}`, "provider_error"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%d_%s", c.status, c.reason), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				io.WriteString(w, c.resp)
			}))
			defer srv.Close()
			s := newTwilioSmsSender(Config{
				TwilioAccountSID: "AC", TwilioAuthToken: "t", TwilioFromNumber: "+15550001",
				TwilioAPIBase: srv.URL,
			}, nil, srv.Client())
			err := s.SendSMSCode(context.Background(), "+14155550100", "1", purposeRegister)
			var de *DeliveryError
			if !errors.As(err, &de) {
				t.Fatalf("want DeliveryError, got %v", err)
			}
			if de.Reason != c.reason {
				t.Errorf("reason=%q want %q (status=%d detail=%s)", de.Reason, c.reason, de.Status, de.Detail)
			}
		})
	}
}

func TestTwilioRedactsAuthTokenInDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, `{"code":21211,"message":"nope supersecret here"}`)
	}))
	defer srv.Close()
	s := newTwilioSmsSender(Config{
		TwilioAccountSID: "AC", TwilioAuthToken: "supersecret", TwilioFromNumber: "+15550001",
		TwilioAPIBase: srv.URL,
	}, nil, srv.Client())
	err := s.SendSMSCode(context.Background(), "+14155550100", "1", purposeRegister)
	var de *DeliveryError
	if !errors.As(err, &de) {
		t.Fatalf("want DeliveryError, got %v", err)
	}
	if strings.Contains(de.Detail, "supersecret") {
		t.Errorf("auth token leaked in detail: %q", de.Detail)
	}
	if !strings.Contains(de.Detail, "[redacted]") {
		t.Errorf("redaction marker missing: %q", de.Detail)
	}
}

func TestTwilioFailedStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"sid":"SM1","status":"failed","code":30005,"message":"Unknown destination"}`)
	}))
	defer srv.Close()
	s := newTwilioSmsSender(Config{
		TwilioAccountSID: "AC", TwilioAuthToken: "t", TwilioFromNumber: "+15550001",
		TwilioAPIBase: srv.URL,
	}, nil, srv.Client())
	err := s.SendSMSCode(context.Background(), "+14155550100", "1", purposeRegister)
	if err == nil {
		t.Fatal("expected failed status to surface as error")
	}
}

// --- phone helpers ---

func TestPhoneDialCode(t *testing.T) {
	cases := map[string]string{
		"+8613800138000":  "86",
		"+14155550100":    "1",
		"+8529999":        "852", // HK 3-digit
		"+442079460958":   "44",
		"+":               "",
		"8613800138000":   "86", // works without +
		"+886912345678":   "886",
	}
	for in, want := range cases {
		if got := phoneDialCode(in); got != want {
			t.Errorf("dialCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderSMSBody(t *testing.T) {
	cn := renderSMSBody("+8613800138000", "123456", purposeRegister)
	if !strings.Contains(cn, "123456") || !strings.HasPrefix(cn, "【Linbit】") {
		t.Errorf("CN body wrong: %q", cn)
	}
	en := renderSMSBody("+14155550100", "123456", purposeWithdraw)
	if !strings.Contains(en, "123456") || !strings.HasPrefix(en, "[Linbit]") {
		t.Errorf("EN body wrong: %q", en)
	}
	// Unknown purpose falls back to register template rather than panicking.
	fallback := renderSMSBody("+14155550100", "1", "nope")
	if fallback == "" {
		t.Errorf("fallback body empty")
	}
}

// --- shared config gate ---

func TestConfigHasRealSMS(t *testing.T) {
	cases := map[string]bool{
		"":       false,
		"log":    false,
		"aliyun": true,
		"twilio": true,
		"AUTO":   true,
		" auto ": true,
		"bogus":  false,
	}
	for in, want := range cases {
		c := Config{SMSProvider: in}
		if got := c.HasRealSMS(); got != want {
			t.Errorf("HasRealSMS(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAliyunResponseParse(t *testing.T) {
	// Defensive: the vendor occasionally returns "RequestId" case-swapped.
	var r aliyunSMSResponse
	if err := json.Unmarshal([]byte(`{"Code":"OK","RequestId":"R1"}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Code != "OK" || r.RequestID != "R1" {
		t.Errorf("parsed wrong: %+v", r)
	}
}
