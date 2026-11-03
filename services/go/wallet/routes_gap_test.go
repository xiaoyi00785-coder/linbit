package main

import "testing"

func TestRoutesRegisterGaps(t *testing.T) {
	s := NewServer(Config{}, nil)
	if s.Routes() == nil {
		t.Fatal("nil handler")
	}
}
