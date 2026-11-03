package main

import (
	"strings"
	"testing"
)

func TestPlacementImageRequired(t *testing.T) {
	for _, slot := range []string{"carousel", "splash", "popup", "grid"} {
		in := placementInput{Slot: slot, Title: "t", Platform: "all"}
		if msg := in.validate(); !strings.Contains(msg, "imageUrl") {
			t.Fatalf("%s without image: got %q", slot, msg)
		}
	}
	for _, slot := range []string{"promo_bar", "search_hint"} {
		in := placementInput{Slot: slot, Title: "t", Platform: "all"}
		if msg := in.validate(); strings.Contains(msg, "imageUrl") {
			t.Fatalf("%s should not require image: %q", slot, msg)
		}
	}
}
