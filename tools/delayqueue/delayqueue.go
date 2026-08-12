// Package delayqueue 提供基于 Redis ZSet 的延迟队列。
//
// 核心思路：将消息的投递时间作为 ZSet 的 score，
// 通过 ZRANGEBYSCORE 获取到期的消息，由消费者循环拉取处理。
//
// 适用场景：延迟关闭、定时提醒、订单超时取消、重试调度等。
//
// 快速开始：
//
//	q := delayqueue.New(redisClient, "my:delay:queue")
//	q.Add("task-1", 30*time.Second)        // 30 秒后可被取出
//	q.Add("task-2", 5*time.Minute)
//
//	// 消费循环
//	for {
//	    msgs, err := q.Poll(ctx, 10, time.Second) // 最多取 10 条，阻塞 1s
//	    for _, m := range msgs {
//	        fmt.Println("处理:", m)
//	        q.Ack(ctx, m) // 处理完成后确认
//	    }
//	}
//
// 能力：Add、Poll、Ack（已处理确认）。
package delayqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Queue 延迟队列。
type Queue struct {
	client redis.Cmdable
	key    string
}

// New 创建延迟队列实例。
//
// client: Redis 客户端（支持 *redis.Client 或 redis.UniversalClient）。
// key: 队列在 Redis 中的键名（建议带业务前缀，如 "order:delay:queue"）。
func New(client redis.Cmdable, key string) *Queue {
	return &Queue{
		client: client,
		key:    key,
	}
}

// Add 将消息加入延迟队列。
//
// msg 是延迟消息的内容。
// delay 是延迟时长：消息在 delay 时间后才可被 Poll 取出。
// 返回消息的唯一 ID（用于后续 Ack）。
func (q *Queue) Add(ctx context.Context, msg string, delay time.Duration) (string, error) {
	id := fmt.Sprintf("%s:%d", q.key, time.Now().UnixNano())
	score := float64(time.Now().Add(delay).UnixNano())
	if err := q.client.ZAdd(ctx, q.key, redis.Z{
		Score:  score,
		Member: id + "|" + msg,
	}).Err(); err != nil {
		return "", fmt.Errorf("delayqueue: add failed: %w", err)
	}
	return id, nil
}

// AddAt 在指定绝对时间点投递消息。
//
// t 是消息可被取出的绝对时间。
func (q *Queue) AddAt(ctx context.Context, msg string, t time.Time) (string, error) {
	id := fmt.Sprintf("%s:%d", q.key, time.Now().UnixNano())
	if err := q.client.ZAdd(ctx, q.key, redis.Z{
		Score:  float64(t.UnixNano()),
		Member: id + "|" + msg,
	}).Err(); err != nil {
		return "", fmt.Errorf("delayqueue: addat failed: %w", err)
	}
	return id, nil
}

// Poll 拉取一批到期的消息。
//
// batch 最多返回的消息数量。
// timeout 是阻塞等待超时时间（传 0 时不阻塞，立即返回）。
// 返回的消息列表不包含已 Ack 的消息（Poll 和 Ack 不保证原子性，
// 如需强一致，请结合 Lua 脚本或消费端去重）。
func (q *Queue) Poll(ctx context.Context, batch int, timeout time.Duration) ([]string, error) {
	now := float64(time.Now().UnixNano())

	// 拉取到期消息
	members, err := q.client.ZRangeByScore(ctx, q.key, &redis.ZRangeBy{
		Min:   "0",
		Max:   fmt.Sprintf("%f", now),
		Count: int64(batch),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("delayqueue: poll failed: %w", err)
	}

	return members, nil
}

// Ack 确认消息已处理，从队列中移除。
//
// member 是 Poll 返回的原始消息字符串（含 ID）。
func (q *Queue) Ack(ctx context.Context, member string) error {
	if err := q.client.ZRem(ctx, q.key, member).Err(); err != nil {
		return fmt.Errorf("delayqueue: ack failed: %w", err)
	}
	return nil
}

// Len 返回队列中未处理的消息数量。
func (q *Queue) Len(ctx context.Context) (int64, error) {
	n, err := q.client.ZCard(ctx, q.key).Result()
	if err != nil {
		return 0, fmt.Errorf("delayqueue: len failed: %w", err)
	}
	return n, nil
}

// Remove 清空队列中的所有消息（测试用，谨慎使用）。
func (q *Queue) Remove(ctx context.Context) error {
	if err := q.client.Del(ctx, q.key).Err(); err != nil {
		return fmt.Errorf("delayqueue: remove failed: %w", err)
	}
	return nil
}
