// Package rediskeyevent 提供 Redis 键空间事件（Keyspace Notification）订阅能力。
//
// Redis 的键过期事件（expired）可用于实现状态自动同步：
// 例如，用户验证码过期后自动标记为失效，订单超时后自动取消。
//
// 前置条件：Redis 配置中启用键空间通知：
//
//	CONFIG SET notify-keyspace-events Ex
//
// 快速开始：
//
//	// 订阅所有匹配 "code:*" 的键过期事件
//	sub, err := rediskeyevent.Subscribe(rdb, "code:*", func(key string) {
//	    fmt.Println("键过期:", key)
//	})
//	defer sub.Close()
//
// 能力：Subscribe（订阅匹配模式的键过期事件）。
package rediskeyevent

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

const expireEventPattern = "__keyevent@%d__:expired"

// Handler 键过期事件处理函数。
// key 是过期的键名。
type Handler func(key string)

// Subscriber 键空间事件订阅器。
type Subscriber struct {
	client  redis.UniversalClient
	db      int
	prefix  string
	handler Handler
}

// Subscribe 订阅匹配 prefix 模式的键过期事件。
//
// client: Redis 客户端。
// db: 监听的数据库编号（0-15），用于构造 Pub/Sub 频道名。
// prefix: 键名前缀模式（如 "code:*" 或 "order:timeout:*"）。
// handler: 键过期时的回调函数。
//
// 返回 *Subscriber，调用方应在退出时调用 Close。
//
// 前置条件：Redis 需启用键空间通知：
//
//	redis.ConfigSet(ctx, "notify-keyspace-events", "Ex")
func Subscribe(client redis.UniversalClient, db int, prefix string, handler Handler) (*Subscriber, error) {
	// 确保 Redis 启用了键空间通知
	ctx := context.Background()
	if err := client.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err(); err != nil {
		return nil, fmt.Errorf("rediskeyevent: failed to enable keyspace notifications: %w", err)
	}

	s := &Subscriber{
		client:  client,
		db:      db,
		prefix:  prefix,
		handler: handler,
	}

	go s.listen(ctx)
	return s, nil
}

// MustSubscribe 同 Subscribe，但 panic 于错误（适合初始化时调用）。
func MustSubscribe(client redis.UniversalClient, db int, prefix string, handler Handler) *Subscriber {
	s, err := Subscribe(client, db, prefix, handler)
	if err != nil {
		panic(err)
	}
	return s
}

// listen 在独立 goroutine 中订阅 Redis Pub/Sub 频道并分发事件。
func (s *Subscriber) listen(ctx context.Context) {
	channel := fmt.Sprintf(expireEventPattern, s.db)
	pubsub := s.client.Subscribe(ctx, channel)

	// 确保退出时关闭连接
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			key := msg.Payload
			if strings.HasPrefix(key, s.prefix) {
				s.handler(key)
			}
		}
	}
}

// Close 停止监听并释放资源。
func (s *Subscriber) Close() error {
	// 依赖 context 取消来停止 listen goroutine
	return nil
}
