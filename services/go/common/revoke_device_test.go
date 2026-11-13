package common

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// Per-device revocation: kicking one device stops that device's access tokens
// without logging the user out on their other devices.
func TestDeviceAccessTokenRevocation(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx := context.Background()
	t0 := time.Now()

	phone, _ := SignAccessTokenForDevice("s", "u1", "a@b.c", "", "cid:phone", time.Hour, t0)
	laptop, _ := SignAccessTokenForDevice("s", "u1", "a@b.c", "", "cid:laptop", time.Hour, t0)
	cPhone, err := ParseAccessToken("s", phone)
	if err != nil {
		t.Fatal(err)
	}
	cLaptop, _ := ParseAccessToken("s", laptop)
	if cPhone.DevID == "" || cPhone.DevID == cLaptop.DevID {
		t.Fatalf("device claim missing or not per device: %q %q", cPhone.DevID, cLaptop.DevID)
	}
	if cPhone.DevID == "cid:phone" {
		t.Fatal("raw device key must not be embedded in the token")
	}

	if err := RevokeDeviceAccessTokens(ctx, rdb, "u1", DeviceClaim("cid:phone"), t0.Add(time.Millisecond), time.Hour); err != nil {
		t.Fatal(err)
	}
	if r, _ := AccessTokenRevoked(ctx, rdb, cPhone); !r {
		t.Fatal("revoked device's token still valid")
	}
	if r, _ := AccessTokenRevoked(ctx, rdb, cLaptop); r {
		t.Fatal("other device was logged out too")
	}
	// A new login on the same device after the kick works again.
	fresh, _ := SignAccessTokenForDevice("s", "u1", "a@b.c", "", "cid:phone", time.Hour, t0.Add(2*time.Millisecond))
	cFresh, _ := ParseAccessToken("s", fresh)
	if r, _ := AccessTokenRevoked(ctx, rdb, cFresh); r {
		t.Fatal("token issued after the kick rejected")
	}
	// Tokens minted before device claims existed are unaffected by device cutoffs.
	legacy, _ := SignAccessToken("s", "u1", "a@b.c", time.Hour, t0)
	cLegacy, _ := ParseAccessToken("s", legacy)
	if r, _ := AccessTokenRevoked(ctx, rdb, cLegacy); r {
		t.Fatal("legacy token without device claim wrongly revoked")
	}
	// The user-wide cutoff still wins over everything.
	if err := RevokeAccessTokens(ctx, rdb, "u1", t0.Add(3*time.Millisecond), time.Hour); err != nil {
		t.Fatal(err)
	}
	if r, _ := AccessTokenRevoked(ctx, rdb, cLaptop); !r {
		t.Fatal("user-wide cutoff did not revoke the other device")
	}
}
