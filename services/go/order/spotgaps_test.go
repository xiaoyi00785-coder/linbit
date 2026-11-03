package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestNormalizeTIF(t *testing.T) {
	for in, want := range map[string]string{"GTX": "PostOnly", "gtx": "PostOnly", "post_only": "PostOnly", "PostOnly": "PostOnly", "gtc": "GTC", "IOC": "IOC", "XYZ": "XYZ"} {
		if got := normalizeTIF(in); got != want {
			t.Errorf("normalizeTIF(%q)=%q want %q", in, got, want)
		}
	}
}

func TestCrossed(t *testing.T) {
	d := decimal.RequireFromString
	if !crossed("up", d("101"), d("100")) || crossed("up", d("99"), d("100")) {
		t.Fatal("up")
	}
	if !crossed("down", d("100"), d("100")) || crossed("down", d("101"), d("100")) {
		t.Fatal("down")
	}
}

func TestUserFeeRate(t *testing.T) {
	s := &Server{cfg: Config{SpotMakerFeeRate: decimal.RequireFromString("0.001"), SpotTakerFeeRate: decimal.RequireFromString("0.002")},
		fees: &feeOverrides{}, ufees: &userFeeCache{m: map[string]userFee{}}}
	if s.userFeeRate("u1", "taker").String() != "0.002" {
		t.Fatal("default")
	}
	m := decimal.RequireFromString("0")
	s.ufees.m["u1"] = userFee{Maker: &m}
	if s.userFeeRate("u1", "maker").String() != "0" || s.userFeeRate("u1", "taker").String() != "0.002" {
		t.Fatal("override")
	}
}
