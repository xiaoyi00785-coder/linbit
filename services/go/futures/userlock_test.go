package main

import "testing"

func TestFutLockKeysSortedAndUnique(t *testing.T) {
	got := futLockKeys("bob", "alice", "bob", "", "carol")
	want := []string{"alice", "bob", "carol"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
