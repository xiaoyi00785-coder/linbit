package common

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// MaybeRedis connects to Redis when url is non-empty and the ping succeeds.
// Redis is an optional dependency: a nil client (url empty or unreachable)
// is a valid, supported configuration and callers must handle it.
func MaybeRedis(url string) *redis.Client {
	if url == "" {
		log.Printf("redis: REDIS_URL not set, running without redis")
		return nil
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		log.Printf("redis: invalid REDIS_URL (%s), continuing without redis", RedactSecrets(err.Error()))
		return nil
	}
	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("redis: ping failed (%s), continuing without redis", RedactSecrets(err.Error()))
		_ = client.Close()
		return nil
	}
	log.Printf("redis: connected to %s", RedactSecrets(url))
	return client
}
