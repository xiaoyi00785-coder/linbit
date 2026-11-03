module github.com/linbit/platform/services/go/account

go 1.27

replace github.com/linbit/platform/services/go/common => ../common

require (
	github.com/alicebob/miniredis/v2 v2.33.0
	github.com/go-webauthn/webauthn v0.18.2
	github.com/jackc/pgx/v5 v5.12.0
	github.com/linbit/platform/services/go/common v0.0.0-00010101000000-000000000000
	github.com/pquerna/otp v1.5.0
	github.com/pressly/goose/v3 v3.29.0
	github.com/redis/go-redis/v9 v9.7.0
	github.com/shopspring/decimal v1.6.0
	golang.org/x/crypto v0.58.0
)

require (
	github.com/alicebob/gopher-json v0.0.0-20200520072559-a9ecdc9d1d3a // indirect
	github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
