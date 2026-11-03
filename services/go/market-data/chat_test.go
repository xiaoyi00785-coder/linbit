package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatViolation(t *testing.T) {
	cfg := defaultChatConfig()
	cases := map[string]string{
		"BTC 要涨到 70000 了":           "",
		"65000 70000 区间震荡":          "",
		"现价 65000.12345，止损 64000":   "",
		"ETH/USDT 这波怎么看？":           "",
		"看这里 https://x.com/abc":     "CHAT_LINK_BLOCKED",
		"www.example.com":           "CHAT_LINK_BLOCKED",
		"ｗｗｗ．ｅｘａｍｐｌｅ．ｃｏｍ":           "CHAT_LINK_BLOCKED",
		"加群 t.me/pumpgroup":         "CHAT_LINK_BLOCKED",
		"mail me foo(at)bar(dot)io": "CHAT_LINK_BLOCKED",
		"联系 abc@qq.com":             "CHAT_LINK_BLOCKED",
		"电话13812345678":             "CHAT_CONTACT_BLOCKED",
		"call +1 415 555 0100":      "CHAT_CONTACT_BLOCKED",
		"微信：wxid_abc123":            "CHAT_CONTACT_BLOCKED",
		"加我QQ 12345678":             "CHAT_CONTACT_BLOCKED",
		"tg @pumpking":              "CHAT_CONTACT_BLOCKED",
		"vx: trader888":             "CHAT_CONTACT_BLOCKED",
	}
	for in, want := range cases {
		if got, _ := chatViolation(in, cfg); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	off := cfg
	off.BlockLinks, off.BlockContacts = false, false
	if got, _ := chatViolation("https://x.com 13812345678", off); got != "" {
		t.Errorf("disabled filters still blocked: %s", got)
	}
	off.BannedWords = []string{"拉盘"}
	if got, _ := chatViolation("一起拉盘", off); got != "CHAT_WORD_BLOCKED" {
		t.Errorf("banned word: %s", got)
	}
}

func TestChatConfigValidate(t *testing.T) {
	if err := defaultChatConfig().validate(); err != nil {
		t.Fatal(err)
	}
	c := defaultChatConfig()
	c.MaxLength = 0
	if c.validate() == nil {
		t.Fatal("maxLength 0 accepted")
	}
}

func TestMaskUID(t *testing.T) {
	if maskUID("10001234") != "用户***1234" {
		t.Fatal(maskUID("10001234"))
	}
}

func TestHubChatChannel(t *testing.T) {
	h := NewHub(nil)
	c := &wsClient{hub: h}
	if code, _ := c.validateChannel("chat:general"); code != "SUB_INVALID" {
		t.Fatalf("no validator: %s", code)
	}
	h.chatValidate = func(room string) (string, string) {
		if room == "general" {
			return "", ""
		}
		return "SUB_INVALID", "x"
	}
	if code, _ := c.validateChannel("chat:general"); code != "" {
		t.Fatal(code)
	}
	if code, _ := c.validateChannel("chat:nope"); code == "" {
		t.Fatal("unknown room accepted")
	}
}

func TestChatMine(t *testing.T) {
	enc := func(m chatMessage) string { b, _ := json.Marshal(m); return string(b) }
	m := chatMessage{ID: 1, Room: "general", Sender: maskUID("u1234"), Text: "hi"}
	if s := enc(m); strings.Contains(s, "mine") {
		t.Errorf("anonymous/broadcast must omit mine: %s", s)
	}
	m.Mine = mineFlag("u1234", "")
	if m.Mine != nil {
		t.Errorf("anonymous requester: want nil")
	}
	m.Mine = mineFlag("u1234", "u1234")
	if s := enc(m); !strings.Contains(s, `"mine":true`) {
		t.Errorf("own message: %s", s)
	}
	m.Mine = mineFlag("u1234", "u9999")
	if s := enc(m); !strings.Contains(s, `"mine":false`) {
		t.Errorf("other's message: %s", s)
	}
}
