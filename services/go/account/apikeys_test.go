package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserAPIKeyReadOnly(t *testing.T) {
	if _, err := userAPIPerms([]string{"read"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range [][]string{{"spot_trade"}, {"futures_trade"}, {"withdraw"}, {"read", "spot_trade"}, nil, {}} {
		if _, err := userAPIPerms(p); err == nil {
			t.Fatalf("accepted %v", p)
		}
	}
	if _, err := normalizeIPs([]string{"10.0.0.1", "2001:db8::1", "10.1.0.0/16"}); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeIPs([]string{"not-an-ip"}); err == nil {
		t.Fatal("hostname accepted")
	}
	if _, err := normalizeIPs([]string{"1.2.3.4/99"}); err == nil {
		t.Fatal("bad cidr")
	}
	item := userKeyDTO{ID: "1", Label: "手机", APIKey: "lbu_abc", Permissions: []string{"read"}, IPWhitelist: []string{"10.0.0.1"}, CreatedAt: 1}
	b, _ := json.Marshal(map[string]any{"items": []userKeyDTO{item}})
	if strings.Contains(string(b), "secret") {
		t.Fatalf("list leaked secret: %s", b)
	}
}
