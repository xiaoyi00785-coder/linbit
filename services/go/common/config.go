// Package common 是各 Go 服务共用的小工具：JWT、JSON/错误响应、环境变量，以及可选的 Redis。
package common

import (
	"log"
	"os"
	"strconv"
)

// Env 返回环境变量；空则用 def。
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// MustEnv 返回环境变量。没设置就退出进程。
func MustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

// EnvInt 把环境变量解析成整数。空或非法则用 def。
func EnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
