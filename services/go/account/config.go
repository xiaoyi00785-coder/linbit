package main

import (
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Config holds the account service runtime configuration, sourced from env.
type Config struct {
	Port             string
	DatabaseURL      string
	JWTSecret        string
	AccessTTL        time.Duration
	RefreshTTL       time.Duration
	ResetTokenTTL    time.Duration
	AssetInternalURL string
	InternalToken    string
	RedisURL         string
	// DevMode exposes the internal "last verification code" endpoint and records
	// plaintext codes in memory. Never enable in production.
	DevMode bool

	// --- avatar / object storage ---
	// StorageBackend selects the ObjectStorage backend: "local" (default) stores
	// avatars on disk; "s3" selects the (skeleton) S3 backend.
	StorageBackend string
	// AvatarDir is the on-disk directory for the local backend.
	AvatarDir string
	// AvatarPublicBase is the URL prefix at which stored avatars are served
	// (the controlled route lives under this prefix on the account service).
	AvatarPublicBase string
	// AvatarMaxBytes caps an uploaded avatar's size (default 2 MiB).
	AvatarMaxBytes int64
	// S3 settings (only used when StorageBackend == "s3").
	S3Bucket     string
	S3Region     string
	S3Endpoint   string
	S3PublicBase string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:             common.Env("PORT", "8081"),
		DatabaseURL:      common.MustEnv("DATABASE_URL"),
		JWTSecret:        common.MustEnv("JWT_SECRET"),
		AccessTTL:        time.Duration(common.EnvInt("ACCESS_TTL_SECONDS", 3600)) * time.Second,
		RefreshTTL:       time.Duration(common.EnvInt("REFRESH_TTL_SECONDS", 7*24*3600)) * time.Second,
		ResetTokenTTL:    time.Duration(common.EnvInt("RESET_TOKEN_TTL_SECONDS", 600)) * time.Second,
		AssetInternalURL: common.Env("ASSET_INTERNAL_URL", ""),
		InternalToken:    common.Env("INTERNAL_TOKEN", ""),
		RedisURL:         common.Env("REDIS_URL", ""),
		DevMode:          common.Env("DEV_MODE", "") == "true",

		StorageBackend:   common.Env("STORAGE_BACKEND", "local"),
		AvatarDir:        common.Env("AVATAR_STORAGE_DIR", "data/avatars"),
		AvatarPublicBase: common.Env("AVATAR_PUBLIC_BASE", "/account/avatars"),
		AvatarMaxBytes:   int64(common.EnvInt("AVATAR_MAX_BYTES", 2*1024*1024)),
		S3Bucket:         common.Env("S3_BUCKET", ""),
		S3Region:         common.Env("S3_REGION", ""),
		S3Endpoint:       common.Env("S3_ENDPOINT", ""),
		S3PublicBase:     common.Env("S3_PUBLIC_BASE", ""),
	}
}
