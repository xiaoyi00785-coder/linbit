package main

import "testing"

func TestRoutesRegister(t *testing.T) {
	s := NewServer(Config{}, &Store{}, nil, nil)
	if s.Routes() == nil {
		t.Fatal("nil handler")
	}
}
