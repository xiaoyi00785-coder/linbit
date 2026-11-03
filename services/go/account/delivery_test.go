package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendErrorsAreDeliveryErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		reason string
	}{
		{401, `{"name":"missing_api_key","message":"Missing API key"}`, "invalid_api_key"},
		{403, `{"name":"validation_error","message":"The cowa.click domain is not verified. Please, add and verify your domain"}`, "from_domain_not_verified"},
		{403, `{"name":"validation_error","message":"You can only send testing emails to your own email address"}`, "sandbox_recipient_only"},
		{422, `{"name":"invalid_from_address","message":"Invalid from field"}`, "invalid_from"},
		{500, `{}`, "provider_error"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			fmt.Fprint(w, c.body+" re_secretKEY")
		}))
		s := newResendSender(srv.URL, "re_secretKEY", "Linbit <no-reply@cowa.click>", nil)
		err := s.SendEmail(context.Background(), "a@b.c", "s", "t")
		srv.Close()
		var de *DeliveryError
		if !errors.As(err, &de) || de.Reason != c.reason || de.Status != c.status {
			t.Fatalf("status %d: got %#v", c.status, err)
		}
		if strings.Contains(err.Error(), "re_secretKEY") {
			t.Fatalf("key leaked: %v", err)
		}
		w := httptest.NewRecorder()
		writeSendCodeError(w, "test", fmt.Errorf("wrap: %w", err))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "EMAIL_UNAVAILABLE") {
			t.Fatalf("mapped to %d %s", w.Code, w.Body.String())
		}
	}
	// network error
	s := newResendSender("http://127.0.0.1:1/emails", "k", "f@cowa.click", nil)
	var de *DeliveryError
	if err := s.SendEmail(context.Background(), "a@b.c", "s", "t"); !errors.As(err, &de) || de.Reason != "network" {
		t.Fatalf("network: %v", err)
	}
}

func TestWriteSendCodeErrorMapping(t *testing.T) {
	w := httptest.NewRecorder()
	writeSendCodeError(w, "t", ErrRateLimited)
	if w.Code != 429 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	writeSendCodeError(w, "t", errors.New("insert code (db): boom"))
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
}

func TestResend422InvalidRecipientIs400(t *testing.T) {
	bodies := []string{
		`{"statusCode":422,"name":"validation_error","message":"Invalid ` + "`to`" + ` field. The email address needs to follow the ` + "`email@example.com`" + ` format."}`,
		`{"statusCode":422,"name":"validation_error","message":"The example.com domain is a testing domain and cannot receive emails"}`,
		`{"statusCode":422,"name":"invalid_to_address","message":"Recipient address is undeliverable"}`,
	}
	for _, body := range bodies {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(422)
			fmt.Fprint(w, body)
		}))
		s := newResendSender(srv.URL, "re_secretKEY", "Linbit <noreply@cowa.click>", nil)
		err := s.SendEmail(context.Background(), "user@example.com", "s", "t")
		srv.Close()
		var de *DeliveryError
		if !errors.As(err, &de) || de.Reason != "invalid_recipient" || de.Status != 422 {
			t.Fatalf("%s: got %#v", body, err)
		}
		w := httptest.NewRecorder()
		writeSendCodeError(w, "test", fmt.Errorf("wrap: %w", err))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "INVALID_EMAIL") {
			t.Fatalf("mapped to %d %s", w.Code, w.Body.String())
		}
	}
}
