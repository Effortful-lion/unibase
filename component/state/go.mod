module github.com/Effortful-lion/unibase/component/state

go 1.26.5

require (
	github.com/Effortful-lion/unibase/component/lockdog v0.0.0
	github.com/Effortful-lion/unibase/logx v0.0.0
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/redis/go-redis/v9 v9.22.0
	golang.org/x/sync v0.22.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/Effortful-lion/unibase/component/lockdog => ../lockdog
	github.com/Effortful-lion/unibase/logx => ../../logx
)
