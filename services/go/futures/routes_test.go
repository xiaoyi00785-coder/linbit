package main

import "testing"

func TestRoutesRegister(t *testing.T) {
	s := &Server{contracts: map[string]Contract{}}
	if s.Routes() == nil {
		t.Fatal("nil handler")
	}
}
