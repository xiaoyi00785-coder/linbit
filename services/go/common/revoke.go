package common

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// 访问令牌吊销。登出时 account 把该用户的截止时间（unix 毫秒）写进 Redis；
// 网关和 account 的鉴权中间件拒绝签发时间不晚于该截止时间的 access token。
// 键的 TTL 只覆盖最长的 access 有效期，检查是一次 Redis GET。

// RevokedBeforeKey 是该用户吊销截止时间的 Redis 键：auth:revoked_before:{uid}。
func RevokedBeforeKey(uid string) string { return "auth:revoked_before:" + uid }

// IssuedAtMs 返回令牌签发时间的 unix 毫秒。没有 iatms 时用秒级 iat，两种都没有则是 0。
func (c *AccessClaims) IssuedAtMs() int64 {
	if c.IatMs > 0 {
		return c.IatMs
	}
	if c.IssuedAt != nil {
		return c.IssuedAt.Time.UnixMilli()
	}
	return 0
}

// RevokeAccessTokens 把 now 记成 uid 的截止时间，保留 ttl。rdb 为 nil 时什么都不做。
func RevokeAccessTokens(ctx context.Context, rdb *redis.Client, uid string, now time.Time, ttl time.Duration) error {
	if rdb == nil {
		return nil
	}
	return rdb.Set(ctx, RevokedBeforeKey(uid), strconv.FormatInt(now.UnixMilli(), 10), ttl).Err()
}

// AccessTokenRevoked 判断令牌是否签发于截止时间或更早（IssuedAtMs <= cutoff）。
// rdb 为 nil，或 Redis 出错，都视为未吊销：出错时返回 false 和 err，避免 Redis 故障把所有人踢下线。
// 键不存在或值无法解析也视为未吊销。调用方应记下 err。
func AccessTokenRevoked(ctx context.Context, rdb *redis.Client, claims *AccessClaims) (bool, error) {
	if rdb == nil || claims == nil {
		return false, nil
	}
	v, err := rdb.Get(ctx, RevokedBeforeKey(claims.Subject)).Result()
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
