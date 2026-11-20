package main

import "testing"

func TestMaskUIDAcademy(t *testing.T) {
	// The comments endpoint publishes commenter uids to anyone; mask all but
	// the first and last three characters so UIDs cannot be enumerated from
	// the comment feed.
	cases := []struct{ in, want string }{
		{"abc", "abc"},                 // too short to mask
		{"abcdefg", "abcdefg"},         // exactly 7, unchanged
		{"abcdefgh", "abc****fgh"},     // just over boundary
		{"u_1234567890", "u_1****890"},
	}
	for _, c := range cases {
		if got := maskUIDAcademy(c.in); got != c.want {
			t.Errorf("maskUIDAcademy(%q)=%q want %q", c.in, got, c.want)
		}
	}
}
