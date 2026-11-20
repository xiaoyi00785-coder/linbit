package main

import (
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Config is the reports service's runtime configuration.
type Config struct {
	Port             string
	DatabaseURL      string
	JWTSecret        string
	InternalToken    string
	DevMode          bool
	ExportsDir       string        // on-disk directory for generated files
	ExportsTTL       time.Duration // file retention
	DownloadBaseURL  string        // prefix put in downloadUrl (e.g. "/reports/exports")
	WatcherTick      time.Duration // deposit watcher interval
	MaxExportRows    int           // per-job row cap
	TaxCountries     []string      // enabled country templates
	DefaultTaxCountry string
}

func LoadConfig() Config {
	return Config{
		Port:              common.Env("PORT", "8098"),
		DatabaseURL:       common.MustEnv("DATABASE_URL"),
		JWTSecret:         common.MustEnv("JWT_SECRET"),
		InternalToken:     common.Env("INTERNAL_TOKEN", ""),
		DevMode:           common.Env("DEV_MODE", "") == "true",
		ExportsDir:        common.Env("REPORTS_EXPORTS_DIR", "data/exports"),
		ExportsTTL:        time.Duration(common.EnvInt("REPORTS_EXPORTS_TTL_HOURS", 7*24)) * time.Hour,
		DownloadBaseURL:   strings.TrimRight(common.Env("REPORTS_DOWNLOAD_BASE_URL", "/reports/exports"), "/"),
		WatcherTick:       time.Duration(common.EnvInt("REPORTS_WATCHER_MS", 5000)) * time.Millisecond,
		MaxExportRows:     common.EnvInt("REPORTS_MAX_ROWS", 200000),
		TaxCountries:      upperCSV(common.Env("REPORTS_TAX_COUNTRIES", "US,DE,JP,CN")),
		DefaultTaxCountry: strings.ToUpper(common.Env("REPORTS_TAX_DEFAULT_COUNTRY", "US")),
	}
}

func upperCSV(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.ToUpper(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
