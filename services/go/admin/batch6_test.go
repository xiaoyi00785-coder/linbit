package main

import (
	"encoding/json"
	"testing"
)

func TestBatch6FundAction(t *testing.T) {
	fa, err := buildFundAction("mm_pair", "spot:btcusdt", json.RawMessage(`{"spreadBps":"12","totpCode":"1","reason":"x","updatedBy":"evil"}`))
	if err != nil || fa.Path != "/internal/mm/pairs/spot/BTCUSDT" || fa.Svc != "mm" {
		t.Fatal(fa, err)
	}
	var m map[string]any
	_ = json.Unmarshal(fa.Body, &m)
	if _, ok := m["totpCode"]; ok || m["updatedBy"] != nil || m["spreadBps"] != "12" {
		t.Fatal(m)
	}
	if _, err := buildFundAction("mm_pair", "xx:BTC", json.RawMessage(`{}`)); err == nil {
		t.Fatal("bad market accepted")
	}
	if _, err := buildFundAction("launchpad_settle", "../x", nil); err == nil {
		t.Fatal("path traversal key accepted")
	}
	fa, _ = buildFundAction("launchpad_distribute", "p1", nil)
	if fa.Path != "/internal/launchpad/projects/p1/distribute" {
		t.Fatal(fa.Path)
	}
	if _, err := buildFundAction("withdraw_anything", "k", json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown target accepted")
	}
	if _, err := buildFundAction("staking_product", "p1", nil); err == nil {
		t.Fatal("missing body accepted")
	}
}

func TestBatch6Helpers(t *testing.T) {
	if !mmPureDisable(map[string]json.RawMessage{"enabled": json.RawMessage("false")}) {
		t.Fatal("disable should be direct")
	}
	if mmPureDisable(map[string]json.RawMessage{"enabled": json.RawMessage("true")}) || mmPureDisable(map[string]json.RawMessage{"enabled": json.RawMessage("false"), "levels": json.RawMessage("3")}) {
		t.Fatal("non-disable must need approval")
	}
	if cmpVer("1.2.10", "1.2.9") != 1 || cmpVer("1.2", "1.2.0") != 0 || cmpVer("0.9", "1.0") != -1 {
		t.Fatal("cmpVer")
	}
	for _, k := range []string{"help_center", "activity:new-year", "page:about"} {
		if !docKeyRe.MatchString(k) {
			t.Fatal(k)
		}
	}
	for _, k := range []string{"../etc", "activity:A B", "x"} {
		if docKeyRe.MatchString(k) {
			t.Fatal(k)
		}
	}
	if !lockedPerms[permConfigApprove] || !lockedPerms[permStrategyForce] {
		t.Fatal("approval / force-stop perms must be locked")
	}
	if can([]string{roleOperations}, permConfigApprove) || !can([]string{roleFinanceSecond}, permConfigApprove) {
		t.Fatal("config:approve matrix")
	}
}

func TestMMPrecheck(t *testing.T) {
	ok := map[string]json.RawMessage{"levels": json.RawMessage(`3`), "maxPosition": json.RawMessage(`"500000"`), "refreshMs": json.RawMessage(`1000`)}
	if err := mmPrecheck(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]json.RawMessage{{"levels": json.RawMessage(`99`)}, {"spreadBps": json.RawMessage(`"0.1"`)}, {"leverage": json.RawMessage(`"x"`)}} {
		if mmPrecheck(bad) == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}
