package websocketx

import (
	"context"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/redis/go-redis/v9"
)

// SessionRegistry 管理用户 Session 到 AP 节点的映射。
type SessionRegistry interface {
	// Register 注册 userID → apAddr 的映射。
	Register(ctx context.Context, group, userID, apAddr string) error
	// Unregister 移除 userID 的映射。
	Unregister(ctx context.Context, group, userID string) error
	// Lookup 查找 userID 对应的 AP 地址，第二个返回值表示是否存在。
	Lookup(ctx context.Context, group, userID string) (string, bool)
}

// redisSessionRegistry 基于 Redis String 实现 Session 注册表。
// Key: session:{group}:{userID} → Value: apAddr
type redisSessionRegistry struct {
	rdb    *redis.Client
	logger *logx.Logger
}

// NewRedisSessionRegistry 创建 Redis 实现的注册表。
func NewRedisSessionRegistry(rdb *redis.Client) SessionRegistry {
	if rdb == nil {
		return &noopSessionRegistry{}
	}
	return &redisSessionRegistry{
		rdb:    rdb,
		logger: logx.Default().Module("mux"),
	}
}

// Register 设置 userID → apAddr 映射（覆盖已存在的映射）。
func (r *redisSessionRegistry) Register(ctx context.Context, group, userID, apAddr string) error {
	key := sessionKey(group, userID)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := r.rdb.Set(ctx, key, apAddr, 0).Err(); err != nil {
		r.logger.Error("session registry register failed", logx.Fields{
			"error":   err,
			"group":   group,
			"user_id": userID,
		})
		return err
	}
	return nil
}

// Unregister 删除 userID 的映射。
func (r *redisSessionRegistry) Unregister(ctx context.Context, group, userID string) error {
	key := sessionKey(group, userID)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := r.rdb.Del(ctx, key).Err(); err != nil {
		r.logger.Warn("session registry unregister failed", logx.Fields{
			"error":   err,
			"group":   group,
			"user_id": userID,
		})
		return err
	}
	return nil
}

// Lookup 查找 userID 对应的 AP 地址。
func (r *redisSessionRegistry) Lookup(ctx context.Context, group, userID string) (string, bool) {
	key := sessionKey(group, userID)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addr, err := r.rdb.Get(ctx, key).Result()
	if err != nil {
		return "", false
	}
	return addr, true
}

func sessionKey(group, userID string) string {
	return "session:" + group + ":" + userID
}

// noopSessionRegistry 空实现的注册表，用于无需 Session 共享的场景。
type noopSessionRegistry struct{}

func (n *noopSessionRegistry) Register(ctx context.Context, group, userID, apAddr string) error {
	return nil
}
func (n *noopSessionRegistry) Unregister(ctx context.Context, group, userID string) error {
	return nil
}
func (n *noopSessionRegistry) Lookup(ctx context.Context, group, userID string) (string, bool) {
	return "", false
}
