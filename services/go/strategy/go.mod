module github.com/linbit/platform/services/go/strategy

go 1.27

replace github.com/linbit/platform/services/go/common => ../common

require (
	github.com/jackc/pgx/v5 v5.12.0
	github.com/linbit/platform/services/go/common v0.0.0-00010101000000-000000000000
	github.com/pressly/goose/v3 v3.29.0
	github.com/shopspring/decimal v1.6.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/redis/go-redis/v9 v9.7.0 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
