package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// twilioSmsSender delivers SMS via Twilio's REST API. The AccountSid plus the
// AuthToken form an HTTP Basic credential; the token is never logged in the
// clear.
type twilioSmsSender struct {
	endpoint    string // https://api.twilio.com/2010-04-01/Accounts/{SID}/Messages.json
	accountSID  string
	authToken   string
	from        string
	messagingSrv string // optional messaging service SID (overrides `from`)
	rec         *CodeRecorder
	http        *http.Client
}

func newTwilioSmsSender(cfg Config, rec *CodeRecorder, client *http.Client) *twilioSmsSender {
	base := cfg.TwilioAPIBase
	if base == "" {
		base = "https://api.twilio.com"
	}
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json",
		strings.TrimRight(base, "/"), url.PathEscape(cfg.TwilioAccountSID))
	return &twilioSmsSender{
		endpoint:     endpoint,
		accountSID:   cfg.TwilioAccountSID,
		authToken:    cfg.TwilioAuthToken,
		from:         cfg.TwilioFromNumber,
		messagingSrv: cfg.TwilioMessagingServiceSID,
		rec:          rec,
		http:         client,
	}
}

func (s *twilioSmsSender) SendSMSCode(ctx context.Context, phone, code, purpose string) error {
	if s.accountSID == "" || s.authToken == "" || (s.from == "" && s.messagingSrv == "") {
		return &DeliveryError{Channel: channelSMS, Reason: "not_configured", Detail: "TWILIO_ACCOUNT_SID/AUTH_TOKEN/FROM_NUMBER empty"}
	}
	body := renderSMSBody(phone, code, purpose)
	form := url.Values{}
	form.Set("To", phone)
	form.Set("Body", body)
	if s.messagingSrv != "" {
		form.Set("MessagingServiceSid", s.messagingSrv)
	} else {
		form.Set("From", s.from)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return &DeliveryError{Channel: channelSMS, Reason: "config", Detail: err.Error()}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.accountSID, s.authToken)

	resp, err := s.http.Do(req)
	if err != nil {
		de := &DeliveryError{Channel: channelSMS, Reason: "network", Detail: err.Error()}
		logSMSFailed("twilio", phone, purpose, de)
		return de
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	var tr twilioMessageResponse
	_ = json.Unmarshal(raw, &tr)

	if resp.StatusCode/100 != 2 {
		detail := redactSecret(string(raw), s.authToken)
		de := &DeliveryError{
			Channel: channelSMS,
			Status:  resp.StatusCode,
			Reason:  twilioReason(resp.StatusCode, tr.Code, tr.Message),
			Detail:  detail,
		}
		logSMSFailed("twilio", phone, purpose, de)
		return de
	}
	// Twilio accepts the request synchronously and then relays. A "failed" or
	// "undelivered" final state is surfaced later via a webhook we don't wire
	// here; "queued"/"accepted"/"sending" are all treated as delivered for the
	// purpose of consuming the rate-limit slot.
	if tr.Status == "failed" || tr.Status == "undelivered" {
		de := &DeliveryError{
			Channel: channelSMS,
			Status:  resp.StatusCode,
			Reason:  twilioReason(resp.StatusCode, tr.Code, tr.Message),
			Detail:  fmt.Sprintf("status=%s code=%d message=%s sid=%s", tr.Status, tr.Code, tr.Message, tr.SID),
		}
		logSMSFailed("twilio", phone, purpose, de)
		return de
	}
	logSMSDelivered("twilio", phone, purpose, tr.SID)
	s.rec.record(phone, purpose, code)
	return nil
}

type twilioMessageResponse struct {
	SID     string `json:"sid"`
	Status  string `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// twilioReason maps a Twilio error response to a short reason label. See
// https://www.twilio.com/docs/api/errors for the code registry.
func twilioReason(status, code int, msg string) string {
	switch code {
	case 20003, 20005:
		return "invalid_api_key"
	case 21211, 21214, 21217, 21604, 21610, 21612:
		return "invalid_recipient"
	case 21408:
		return "region_not_permitted"
	case 20429, 63038:
		return "provider_rate_limited"
	case 30003, 30004, 30005, 30006:
		return "undeliverable_recipient"
	}
	low := strings.ToLower(msg)
	switch {
	case status == 401 || status == 403:
		return "invalid_api_key"
	case strings.Contains(low, "to number") || strings.Contains(low, "'to'"):
		return "invalid_recipient"
	case strings.Contains(low, "unverified"):
		return "trial_unverified_recipient"
	case status == 429:
		return "provider_rate_limited"
	case status >= 500:
		return "provider_error"
	}
	return fmt.Sprintf("http_%d", status)
}
