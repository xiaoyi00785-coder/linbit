package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

var httpc = &http.Client{Timeout: 10 * time.Second}

type apiErr struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiErr) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

// call performs an internal request (X-Internal-Token) and decodes JSON.
func (s *Server) call(ctx context.Context, method, u string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		e := &apiErr{Status: resp.StatusCode}
		_ = json.NewDecoder(resp.Body).Decode(e)
		return e
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// userTransfer moves `amount` of `asset` from `fromUID`'s spot balance to
// `toUID`'s spot balance; idempotent on ref.
func (s *Server) userTransfer(ctx context.Context, fromUID, toUID, asset string, amount decimal.Decimal, ref string) error {
	return s.call(ctx, "POST", s.cfg.AssetURL+"/internal/user-transfer", map[string]string{
		"userId": fromUID, "toUserId": toUID, "asset": asset,
		"amount": amount.String(), "account": "spot", "ref": ref,
	}, nil)
}
