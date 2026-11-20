package main

import "testing"

func TestParseInt64(t *testing.T) {
	cases := map[string]int64{"0": 0, "123": 123, "-7": -7, "+42": 42}
	for in, want := range cases {
		got, err := parseInt64(in)
		if err != nil || got != want {
			t.Fatalf("%q: got %d err=%v", in, got, err)
		}
	}
	if _, err := parseInt64(""); err == nil {
		t.Fatal("empty should error")
	}
	if _, err := parseInt64("abc"); err == nil {
		t.Fatal("non-numeric should error")
	}
}

func TestValidKinds(t *testing.T) {
	for _, k := range []string{"text", "image", "audio", "video", "custom", "system", "redpacket"} {
		if !validKinds[k] {
			t.Fatalf("%s should be valid", k)
		}
	}
	if validKinds["javascript"] {
		t.Fatal("unknown kind should be rejected")
	}
}

func TestRandomIDPositive(t *testing.T) {
	for i := 0; i < 1000; i++ {
		if n := randomID(); n <= 0 {
			t.Fatalf("got non-positive %d", n)
		}
	}
}
