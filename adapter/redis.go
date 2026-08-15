package adapter

import (
	"context"
	"time"

	"github.com/Effortful-lion/unibase/logx"

	"github.com/redis/go-redis/v9"
)

// RedisConfig Redis 连接配置。
type RedisConfig struct {
	// Addr 服务器地址，必填。例如 "localhost:6379"。
	Addr string

	// Password 密码，无密码时留空。
	Password string

	// DB 数据库编号，默认 0。
	DB int

	// MaxRetry 最大重试次数，0 表示使用默认值 3。
	MaxRetry int

	// DialTimeout 连接超时，0 表示使用默认值 5s。
	DialTimeout time.Duration

	// ReadTimeout 读超时，0 表示使用默认值 3s。
	ReadTimeout time.Duration

	// WriteTimeout 写超时，0 表示使用默认值 3s。
	WriteTimeout time.Duration

	// PoolSize 连接池大小，0 表示使用默认值 10 * GOMAXPROCS。
	PoolSize int
}

// Redis 是 Redis 客户端的薄封装。
// 持有标准库的 *redis.Client，核心能力直接委托给原始客户端。
type Redis struct {
	client *redis.Client
	logger *logx.Logger
}

// NewRedis 创建 Redis 适配器。
func NewRedis(cfg RedisConfig) *Redis {
	opt := redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolSize:     cfg.PoolSize,
	}

	if opt.DialTimeout == 0 {
		opt.DialTimeout = 5 * time.Second
	}
	if opt.ReadTimeout == 0 {
		opt.ReadTimeout = 3 * time.Second
	}
	if opt.WriteTimeout == 0 {
		opt.WriteTimeout = 3 * time.Second
	}
	if cfg.MaxRetry > 0 {
		opt.MaxRetries = cfg.MaxRetry
	}

	return &Redis{
		client: redis.NewClient(&opt),
		logger: logx.Module("adapter.redis"),
	}
}

// Client 返回底层的 *redis.Client，可直接使用 go-redis/v9 的全部能力。
//
//	rdb := redisAdapter.Client()
//	err := rdb.Set(ctx, "key", "value", 0).Err()
func (r *Redis) Client() *redis.Client { return r.client }

// Ping 检查 Redis 连接是否可用。
func (r *Redis) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Close 关闭 Redis 连接池。
func (r *Redis) Close() error {
	return r.client.Close()
}
