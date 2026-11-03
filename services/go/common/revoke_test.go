package common

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestAccessTokenRevocation(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx := context.Background()
	t0 := time.Now()
	old, _ := SignAccessToken("s", "u1", "a@b.c", time.Hour, t0)
	c, err := ParseAccessToken("s", old)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := AccessTokenRevoked(ctx, rdb, c); r {
		t.Fatal("revoked before logout")
	}
	if err := RevokeAccessTokens(ctx, rdb, "u1", t0.Add(time.Millisecond), time.Hour); err != nil {
		t.Fatal(err)
	}
	if r, _ := AccessTokenRevoked(ctx, rdb, c); !r {
		t.Fatal("old token still valid after logout")
	}
	fresh, _ := SignAccessToken("s", "u1", "a@b.c", time.Hour, t0.Add(2*time.Millisecond))
	c2, _ := ParseAccessToken("s", fresh)
	if r, _ := AccessTokenRevoked(ctx, rdb, c2); r {
		t.Fatal("token issued after logout rejected")
	}
	other, _ := SignAccessToken("s", "u2", "x@b.c", time.Hour, t0)
	c3, _ := ParseAccessToken("s", other)
	if r, _ := AccessTokenRevoked(ctx, rdb, c3); r {
		t.Fatal("other user affected")
	}
	if ttl := mr.TTL(RevokedBeforeKey("u1")); ttl <= 0 || ttl > time.Hour {
		t.Fatalf("ttl %v", ttl)
	}
	if r, err := AccessTokenRevoked(ctx, nil, c); r || err != nil {
		t.Fatal("nil client must fail open")
	}
}
