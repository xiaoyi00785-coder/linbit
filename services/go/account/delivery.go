package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// DeliveryError is returned by an email/SMS sender when the upstream provider
// could not accept the message. It never carries the API key.
type DeliveryError struct {
	Channel string // email | sms
	Status  int    // upstream HTTP status, 0 for network/config errors
	Reason  string // invalid_api_key, from_domain_not_verified, network, ...
	Detail  string
}

func (e *DeliveryError) Error() string {
	return fmt.Sprintf("%s delivery failed: reason=%s status=%d detail=%s", e.Channel, e.Reason, e.Status, e.Detail)
}

// writeSendCodeError maps a CodeService.Send error to an API response and logs
// the cause (without secrets). Delivery failures are 503 *_UNAVAILABLE so the
// client can show "try later" instead of a generic 500. A provider 4xx that
// blames the recipient address (reason invalid_recipient) is 400 INVALID_EMAIL.
func writeSendCodeError(w http.ResponseWriter, where string, err error) {
	if errors.Is(err, ErrRateLimited) {
		common.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "验证码发送过于频繁，请稍后再试")
		return
	}
	log.Printf("%s: send code: %v", where, err)
	var de *DeliveryError
	if errors.As(err, &de) {
		if de.Channel == channelEmail && de.Reason == "invalid_recipient" {
			common.WriteError(w, http.StatusBadRequest, "INVALID_EMAIL", "邮箱地址无效或无法接收邮件，请检查后重试")
			return
		}
		if de.Channel == channelSMS {
			common.WriteError(w, http.StatusServiceUnavailable, "SMS_UNAVAILABLE", "短信服务暂不可用，请稍后再试")
			return
		}
		common.WriteError(w, http.StatusServiceUnavailable, "EMAIL_UNAVAILABLE", "邮件服务暂不可用，请稍后再试")
		return
	}
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}
