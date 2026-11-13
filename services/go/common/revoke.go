package common

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Access-token revocation. On logout the account service records a per-user
// cutoff (unix ms) in Redis; the gateway (and account auth middleware) reject
// any access token issued before it. The key lives only as long as the longest
// possible access-token lifetime, so the check is one O(1) Redis GET.

// RevokedBeforeKey is the Redis key holding a user's revocation cutoff.
func RevokedBeforeKey(uid string) string { return "auth:revoked_before:" + uid }

// RevokedDeviceKey is the Redis key holding one device's revocation cutoff.
func RevokedDeviceKey(uid, deviceClaim string) string {
	return "auth:revoked_device:" + uid + ":" + deviceClaim
}

// RevokeDeviceAccessTokens stores now as the cutoff for one device's access
// tokens (tokens carry the device claim; see AccessClaims.DevID).
func RevokeDeviceAccessTokens(ctx context.Context, rdb *redis.Client, uid, deviceClaim string, now time.Time, ttl time.Duration) error {
	if rdb == nil || deviceClaim == "" {
		return nil
	}
	return rdb.Set(ctx, RevokedDeviceKey(uid, deviceClaim), strconv.FormatInt(now.UnixMilli(), 10), ttl).Err()
}

// deviceRevoked reports whether the token's device was revoked after issue.
func deviceRevoked(ctx context.Context, rdb *redis.Client, claims *AccessClaims) (bool, error) {
	if claims.DevID == "" {
		return false, nil
	}
	v, err := rdb.Get(ctx, RevokedDeviceKey(claims.Subject, claims.DevID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cut, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return false, nil
	}
	return claims.IssuedAtMs() <= cut, nil
}

// IssuedAtMs returns the token's issue time in unix milliseconds (falls back
// to the second-precision iat for tokens minted before the iatms claim).
func (c *AccessClaims) IssuedAtMs() int64 {
	if c.IatMs > 0 {
		return c.IatMs
	}
	if c.IssuedAt != nil {
		return c.IssuedAt.Time.UnixMilli()
	}
	return 0
}

// RevokeAccessTokens stores now as the cutoff for uid, kept for ttl.
func RevokeAccessTokens(ctx context.Context, rdb *redis.Client, uid string, now time.Time, ttl time.Duration) error {
	if rdb == nil {
		return nil
	}
	return rdb.Set(ctx, RevokedBeforeKey(uid), strconv.FormatInt(now.UnixMilli(), 10), ttl).Err()
}

// AccessTokenRevoked reports whether claims were issued at or before the
// user's revocation cutoff, or at or before the cutoff of the token's own
// device (a single session kicked). A nil client or Redis error fails open
// (returns false, err) so a Redis outage does not log everyone out; callers
// log err.
func AccessTokenRevoked(ctx context.Context, rdb *redis.Client, claims *AccessClaims) (bool, error) {
	if rdb == nil || claims == nil {
		return false, nil
	}
	v, err := rdb.Get(ctx, RevokedBeforeKey(claims.Subject)).Result()
	if errors.Is(err, redis.Nil) {
		// No user-wide cutoff; the device cutoff may still apply.
		return deviceRevoked(ctx, rdb, claims)
	}
	if err != nil {
		return false, err
	}
	cut, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return false, nil
	}
	if claims.IssuedAtMs() <= cut {
		return true, nil
	}
	return deviceRevoked(ctx, rdb, claims)
}
