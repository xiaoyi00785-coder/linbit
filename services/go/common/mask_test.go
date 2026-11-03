package common

import "testing"

func TestMaskEmail(t *testing.T) {
	for in, want := range map[string]string{
		"xiaoyi@example.com": "xia***@example.com",
		"alice@example.com":  "ali***@example.com",
		"abc@x.io":           "ab***@x.io",
		"ab@x.io":            "a***@x.io",
		"a@x.io":             "a***@x.io",
		"bad":                "bad",
	} {
		if got := MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMaskPhone(t *testing.T) {
	for in, want := range map[string]string{
		"13812345678": "138****5678",
		"1234567":     "12***67",
		"12345":       "12***45",
		"1234":        "****",
	} {
		if got := MaskPhone(in); got != want {
			t.Errorf("MaskPhone(%q)=%q want %q", in, got, want)
		}
	}
}
