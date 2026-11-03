package main

import "github.com/linbit/platform/services/go/common"

// Config holds the KYC service runtime configuration, sourced from env.
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	RedisURL      string

	// AccountInternalURL is the account service base URL used to sync the user's
	// kycLevel after an application is approved (service-to-service, internal token).
	AccountInternalURL string

	// MinAge is the minimum age (in whole years) an applicant must have reached by
	// the submission date. Product default is 18
	// (see summary). Configurable via KYC_MIN_AGE.
	MinAge int

	// --- document object storage (reuses the shared ObjectStorage) ---
	// StorageBackend selects the backend: "local" (default, on disk) or "s3".
	StorageBackend string
	// DocDir is the on-disk directory for the local backend.
	DocDir string
	// DocPublicBase is the key prefix / URL base the backend maps keys under. KYC
	// documents are never served publicly (see server.go); this only shapes the
	// backend's own URL bookkeeping.
	DocPublicBase string
	// DocMaxBytes caps an uploaded document's size. Default 10 MiB, matching the
	// web UI's client-side limit. Configurable via KYC_DOC_MAX_BYTES.
	DocMaxBytes int64
	// S3 settings (only used when StorageBackend == "s3").
	S3Bucket     string
	S3Region     string
	S3Endpoint   string
	S3PublicBase string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:               common.Env("PORT", "8087"),
		DatabaseURL:        common.MustEnv("DATABASE_URL"),
		InternalToken:      common.Env("INTERNAL_TOKEN", ""),
		RedisURL:           common.Env("REDIS_URL", ""),
		AccountInternalURL: common.Env("ACCOUNT_INTERNAL_URL", ""),
		MinAge:             common.EnvInt("KYC_MIN_AGE", 18),

		StorageBackend: common.Env("STORAGE_BACKEND", "local"),
		DocDir:         common.Env("KYC_DOC_STORAGE_DIR", "data/kyc-docs"),
		DocPublicBase:  common.Env("KYC_DOC_PUBLIC_BASE", "/kyc/documents"),
		DocMaxBytes:    int64(common.EnvInt("KYC_DOC_MAX_BYTES", 10*1024*1024)),
		S3Bucket:       common.Env("S3_BUCKET", ""),
		S3Region:       common.Env("S3_REGION", ""),
		S3Endpoint:     common.Env("S3_ENDPOINT", ""),
		S3PublicBase:   common.Env("S3_PUBLIC_BASE", ""),
	}
}
