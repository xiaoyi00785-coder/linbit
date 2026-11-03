package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestValidateTransfer(t *testing.T) {
	min := decimal.RequireFromString("0.01")
	ok := []struct {
		req transferReq
		dir string
	}{
		{transferReq{Asset: "usdt", Amount: "10", From: "Spot", To: "futures", ClientID: "c-1"}, "in"},
		{transferReq{Asset: "USDT", Amount: "0.01", From: "futures", To: "spot", ClientID: "abc_2:x.y"}, "out"},
		{transferReq{Asset: "USDT", Amount: "5", From: "funding", To: "futures", ClientID: "f-1"}, "in"},
		{transferReq{Asset: "USDT", Amount: "5", From: "futures", To: "funding", ClientID: "f-2"}, "out"},
		{transferReq{Asset: "btc", Amount: "0.00001", From: "funding", To: "spot", ClientID: "f-3"}, "internal"},
		{transferReq{Asset: "ETH", Amount: "1", From: "spot", To: "funding", ClientID: "f-4"}, "internal"},
	}
	for _, c := range ok {
		dir, _, err := validateTransfer(&c.req, min)
		if err != nil || dir != c.dir {
			t.Errorf("%+v: dir=%s err=%v", c.req, dir, err)
		}
	}
	bad := []struct {
		req  transferReq
		code string
	}{
		{transferReq{Asset: "USDT", Amount: "1", From: "spot", To: "spot", ClientID: "c"}, "SAME_ACCOUNT"},
		{transferReq{Asset: "USDT", Amount: "1", From: "spot", To: "earn", ClientID: "c"}, "ACCOUNT_NOT_SUPPORTED"},
		{transferReq{Asset: "USDT", Amount: "1", From: "wallet9", To: "spot", ClientID: "c"}, "ACCOUNT_NOT_SUPPORTED"},
		{transferReq{Asset: "USDT", Amount: "1", From: "spot", To: "futures"}, "VALIDATION_ERROR"},
		{transferReq{Asset: "USDT", Amount: "1", From: "spot", To: "futures", ClientID: "has space"}, "VALIDATION_ERROR"},
		{transferReq{Asset: "USDT", Amount: "-1", From: "spot", To: "futures", ClientID: "c"}, "VALIDATION_ERROR"},
		{transferReq{Asset: "USDT", Amount: "0.001", From: "spot", To: "futures", ClientID: "c"}, "VALIDATION_ERROR"},
		{transferReq{Asset: "USDT", Amount: "1.123456789", From: "spot", To: "futures", ClientID: "c"}, "VALIDATION_ERROR"},
		{transferReq{Asset: "USDT", Amount: "abc", From: "spot", To: "futures", ClientID: "c"}, "VALIDATION_ERROR"},
		{transferReq{Asset: "BTC", Amount: "1", From: "spot", To: "futures", ClientID: "c"}, "VALIDATION_ERROR"},
		{transferReq{Asset: "", Amount: "1", From: "spot", To: "futures", ClientID: "c"}, "VALIDATION_ERROR"},
		{transferReq{Asset: "BTC", Amount: "1", From: "funding", To: "futures", ClientID: "c"}, "VALIDATION_ERROR"},
		{transferReq{Asset: "BTC", Amount: "1", From: "funding", To: "funding", ClientID: "c"}, "SAME_ACCOUNT"},
		{transferReq{Asset: "B$C", Amount: "1", From: "funding", To: "spot", ClientID: "c"}, "VALIDATION_ERROR"},
	}
	for _, c := range bad {
		_, _, err := validateTransfer(&c.req, min)
		a, ok := asAPI(err)
		if !ok || a.Code != c.code {
			t.Errorf("%+v: want %s got %v", c.req, c.code, err)
		}
	}
}
