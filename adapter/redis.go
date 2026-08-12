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

// ==================== 快捷操作 ====================

// Get 获取 key 的值，不存在时返回 redis.Nil。
func (r *Redis) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

// Set 设置 key 的值为 val，expiration 为过期时间（0 表示不过期）。
func (r *Redis) Set(ctx context.Context, key, val string, expiration time.Duration) error {
	return r.client.Set(ctx, key, val, expiration).Err()
}

// SetEX 设置 key 的值并指定过期时间（秒）。
func (r *Redis) SetEX(ctx context.Context, key, val string, seconds int) error {
	return r.client.Set(ctx, key, val, time.Duration(seconds)*time.Second).Err()
}

// Del 删除一个或多个 key。
func (r *Redis) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

// Exists 判断 key 是否存在，返回存在的 key 数量。
func (r *Redis) Exists(ctx context.Context, keys ...string) (int64, error) {
	return r.client.Exists(ctx, keys...).Result()
}

// MGet 批量获取多个 key 的值。
// 返回的切片长度与 keys 一致，不存在的 key 对应 nil。
func (r *Redis) MGet(ctx context.Context, keys ...string) ([]any, error) {
	return r.client.MGet(ctx, keys...).Result()
}

// Incr 对 key 的整数值加 1。
func (r *Redis) Incr(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

// Decr 对 key 的整数值减 1。
func (r *Redis) Decr(ctx context.Context, key string) (int64, error) {
	return r.client.Decr(ctx, key).Result()
}

// Expire 设置 key 的过期时间（秒）。
func (r *Redis) Expire(ctx context.Context, key string, seconds int) error {
	return r.client.Expire(ctx, key, time.Duration(seconds)*time.Second).Err()
}

// TTL 获取 key 的剩余过期时间（秒）。
func (r *Redis) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}
