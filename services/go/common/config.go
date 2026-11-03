// Package common holds helpers shared by the Linbit Go services: JWT handling,
// JSON/error responses, env configuration and an optional Redis dependency.
package common

import (
	"log"
	"os"
	"strconv"
)

// Env returns the environment variable or the provided default.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// MustEnv returns the environment variable or terminates the process.
func MustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

// EnvInt returns the integer env var or the provided default.
func EnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
