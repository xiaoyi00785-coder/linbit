module github.com/linbit/platform/services/go/gateway

go 1.27

replace github.com/linbit/platform/services/go/common => ../common

require (
	github.com/alicebob/miniredis/v2 v2.33.0
	github.com/linbit/platform/services/go/common v0.0.0-00010101000000-000000000000
	github.com/redis/go-redis/v9 v9.7.0
)

require (
	github.com/alicebob/gopher-json v0.0.0-20200520072559-a9ecdc9d1d3a // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
)
