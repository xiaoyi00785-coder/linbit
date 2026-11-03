module github.com/linbit/platform/services/go/gateway

go 1.27

replace github.com/linbit/platform/services/go/common => ../common

require github.com/linbit/platform/services/go/common v0.0.0-00010101000000-000000000000

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/redis/go-redis/v9 v9.7.0 // indirect
)
