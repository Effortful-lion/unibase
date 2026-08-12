module github.com/Effortful-lion/unibase/tools/delayqueue

go 1.26.5

require (
	github.com/Effortful-lion/unibase/component/lockdog v0.0.0-20260811204407-5d82c26ced18
	github.com/Effortful-lion/unibase/tools/id v0.0.0-20260811204407-5d82c26ced18
	github.com/redis/go-redis/v9 v9.22.0
)

replace github.com/Effortful-lion/unibase/component/lockdog => ../../component/lockdog

replace github.com/Effortful-lion/unibase/tools/id => ../id

require (
	github.com/bwmarrin/snowflake v0.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/matoous/go-nanoid/v2 v2.1.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)
