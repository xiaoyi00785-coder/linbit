package common

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// MaybeRedis 在 url 非空且 ping 成功时返回客户端。
// url 为空、解析失败或 ping 失败都返回 nil 并记日志。这是允许的配置，调用方要处理 nil。
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
