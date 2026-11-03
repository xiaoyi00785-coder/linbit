package main

import (
	"strings"
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
	// OrderInternalURL lets a parent read its sub-accounts' spot orders.
	OrderInternalURL string
	InternalToken    string
	RedisURL         string
	// DevMode exposes the internal "last verification code" endpoint and records
	// plaintext codes in memory. Never enable in production.
	DevMode bool

	// --- security settings ---
	// SMSEnabled: a real SMS provider is wired. When false (and not DevMode)
	// SMS-dependent flows return 503 SMS_UNAVAILABLE.
	SMSEnabled bool
	// WebAuthn relying party (passkeys). RPID e.g. "cowa.click"; empty disables passkeys.
	WebAuthnRPID      string
	WebAuthnRPName    string
	WebAuthnRPOrigins []string

	// --- email provider (Resend). Both empty -> dev mock sender. ---
	ResendAPIKey string
	ResendFrom   string
	ResendURL    string

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
		Port:              common.Env("PORT", "8081"),
		DatabaseURL:       common.MustEnv("DATABASE_URL"),
		JWTSecret:         common.MustEnv("JWT_SECRET"),
		AccessTTL:         time.Duration(common.EnvInt("ACCESS_TTL_SECONDS", 3600)) * time.Second,
		RefreshTTL:        time.Duration(common.EnvInt("REFRESH_TTL_SECONDS", 7*24*3600)) * time.Second,
		ResetTokenTTL:     time.Duration(common.EnvInt("RESET_TOKEN_TTL_SECONDS", 600)) * time.Second,
		AssetInternalURL:  common.Env("ASSET_INTERNAL_URL", ""),
		OrderInternalURL:  common.Env("ORDER_INTERNAL_URL", ""),
		InternalToken:     common.Env("INTERNAL_TOKEN", ""),
		RedisURL:          common.Env("REDIS_URL", ""),
		DevMode:           common.Env("DEV_MODE", "") == "true",
		SMSEnabled:        common.Env("SMS_ENABLED", "") == "true",
		WebAuthnRPID:      common.Env("WEBAUTHN_RP_ID", ""),
		WebAuthnRPName:    common.Env("WEBAUTHN_RP_NAME", "Linbit"),
		WebAuthnRPOrigins: splitCSV(common.Env("WEBAUTHN_RP_ORIGINS", common.Env("WEBAUTHN_ORIGINS", ""))), // WEBAUTHN_ORIGINS: legacy alias,
		ResendAPIKey:      common.Env("RESEND_API_KEY", ""),
		ResendFrom:        common.Env("RESEND_FROM", ""),
		ResendURL:         common.Env("RESEND_API_URL", "https://api.resend.com/emails"),

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

func splitCSV(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
