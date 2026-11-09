package main

import "testing"

func TestAllowFuturesTicker(t *testing.T) {
	if !allowFuturesTicker("crypto", false, "") || allowFuturesTicker("metal", false, "") || allowFuturesTicker("commodity", false, "") {
		t.Fatal("default futures ticker must be crypto perpetuals only")
	}
	if !allowFuturesTicker("metal", true, "") || !allowFuturesTicker("commodity", true, "") {
		t.Fatal("include=tradfi")
	}
	if !allowFuturesTicker("metal", false, "metal") || allowFuturesTicker("crypto", false, "metal") {
		t.Fatal("kind=metal")
	}
	if !validInstrumentKind("commodity") || validInstrumentKind("index") {
		t.Fatal("kind validation")
	}
}
