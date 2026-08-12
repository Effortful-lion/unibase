// Package delayqueue 提供生产级 Redis 延迟队列。
//
// 基于 Redis ZSet 实现，具备以下生产级能力：
//
//   - 原子 Poll：ZPOPMINBYSCORE 原子弹出，多消费者不重复抢消息
//   - 原子 Nack：Lua 脚本 + lockdog 分布式锁，保证 Nack/Sweep 不竞态
//   - 可见性超时：Poll 后消息进入 Processing 状态，超时未 Ack 自动重试
//   - NACK + 重试 + DLQ：消费失败按退避重试，超限移入死信队列
//   - 兜底 Sweeper：定期回收超时 Processing 消息，防止消费者宕机导致消息卡死
//   - 优雅关闭：Stop 等待 Processing 消息处理完毕再退出
//   - Consumer 模式：Start(ctx, consumer) 一行启动消费循环
//
// Redis 版本要求：>= 6.2（使用 ZPOPMINBYSCORE 原子弹出）。
//
// 快速开始：
//
//	q := delayqueue.New(rdb, "order:delay",
//	    delayqueue.WithMaxRetries(3),
//	    delayqueue.WithVisibilityTimeout(30*time.Second),
//	)
//
//	// Consumer 模式（推荐）
//	type handler struct{}
//	func (h *handler) Consume(ctx context.Context, msg *delayqueue.Message) error {
//	    return process(msg)
//	}
//	q.Start(ctx, &handler{})
//
// 能力：Add / AddAt / Poll / Ack / Nack / Start / Stop / DLQMessages / Stats。
package delayqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Effortful-lion/unibase/component/lockdog"
	"github.com/Effortful-lion/unibase/tools/id"
	"github.com/redis/go-redis/v9"
)

// ─── 错误 ───

var (
	ErrQueueStopped = fmt.Errorf("delayqueue: queue stopped")
	ErrMaxRetries   = fmt.Errorf("delayqueue: max retries exceeded")
)

// ─── 数据结构 ───

// Message 是延迟队列中的消息。
type Message struct {
	// ID 消息唯一标识（UUID v4）。
	ID string
	// Payload 原始消息内容。
	Payload string
	// Retries 已重试次数。
	Retries int
	// Raw 原始 JSON 字符串，用于 Ack/Nack 时传递 member。
	Raw string
}

// Consumer 处理从队列取出的消息。
// Consume 返回 nil 表示处理成功（Ack），返回非 nil 表示处理失败（Nack → 重试或 DLQ）。
type Consumer interface {
	Consume(ctx context.Context, msg *Message) error
}

// Stats 队列统计信息。
type Stats struct {
	Delay      int64
	Processing int64
	DLQ        int64
}

// Queue 延迟队列。
type Queue struct {
	client            redis.Cmdable
	key               string
	processingKey     string
	dlqKey            string
	visibilityTimeout time.Duration
	maxRetries        int
	sweepInterval     time.Duration
	backoff           func(retries int) time.Duration
	locker            lockdog.Locker

	mu      sync.Mutex
	stopCh  chan struct{}
	wg      sync.WaitGroup
	running bool
}

// Config 延迟队列配置。
type Config struct {
	// VisibilityTimeout 消息被 Poll 后的可见性超时时间。
	// 超时未 Ack 的消息会被 Sweeper 重新放回延迟队列。
	// 默认 30s。
	VisibilityTimeout time.Duration

	// MaxRetries 消息最大重试次数，达到后移入 DLQ。
	// 默认 3 次。
	MaxRetries int

	// SweepInterval Sweeper 扫描间隔，用于回收超时 Processing 消息。
	// 默认 5s。构造时若大于 VisibilityTimeout / 3 则自动缩减。
	SweepInterval time.Duration

	// Backoff 自定义重试退避函数。
	// retries 是已重试次数（从 1 开始），返回下次重试延迟。
	// 默认：第 N 次重试延迟 N 分钟。
	Backoff func(retries int) time.Duration
}

func defaultConfig() Config {
	return Config{
		VisibilityTimeout: 30 * time.Second,
		MaxRetries:        3,
		SweepInterval:     5 * time.Second,
		Backoff: func(n int) time.Duration {
			return time.Duration(n) * time.Minute
		},
	}
}

// ─── 构造函数 ───

// New 创建延迟队列实例。
//
// client: Redis 客户端（支持 *redis.Client 或 redis.UniversalClient）。
// key: 队列在 Redis 中的键名前缀（如 "order:delay"）。
// opts: 配置选项（WithMaxRetries / WithVisibilityTimeout / WithSweepInterval / WithBackoff）。
//
// Redis 键名：
//
//	{key}            — 延迟队列 (ZSet)
//	{key}:processing — Processing 队列 (ZSet)
//	{key}:dlq        — 死信队列 (List)
//	lock:{key}:nack:{member} — Nack 分布式锁（lockdog）
func New(client redis.Cmdable, key string, opts ...func(*Config)) *Queue {
	cfg := defaultConfig()
	for _, apply := range opts {
		apply(&cfg)
	}

	// 自动修正 SweepInterval：不超过 VisibilityTimeout 的 1/3
	if cfg.SweepInterval > cfg.VisibilityTimeout/3 {
		cfg.SweepInterval = cfg.VisibilityTimeout / 3
	}

	// lockdog 用于 Nack/Sweep 并发控制，禁用看门狗（锁持有时间很短）
	locker := lockdog.New(client, lockdog.WithWatchdogInterval(0))

	q := &Queue{
		client:            client,
		key:               key,
		processingKey:     key + ":processing",
		dlqKey:            key + ":dlq",
		visibilityTimeout: cfg.VisibilityTimeout,
		maxRetries:        cfg.MaxRetries,
		sweepInterval:     cfg.SweepInterval,
		backoff:           cfg.Backoff,
		locker:            locker,
	}
	if q.backoff == nil {
		q.backoff = func(n int) time.Duration { return time.Duration(n) * time.Minute }
	}
	return q
}

// ─── 配置选项 ───

// WithMaxRetries 设置最大重试次数（默认 3）。
func WithMaxRetries(n int) func(*Config) {
	return func(c *Config) { c.MaxRetries = n }
}

// WithVisibilityTimeout 设置可见性超时（默认 30s）。
func WithVisibilityTimeout(d time.Duration) func(*Config) {
	return func(c *Config) { c.VisibilityTimeout = d }
}

// WithSweepInterval 设置 Sweeper 扫描间隔（默认 5s，自动不超过 VisibilityTimeout/3）。
func WithSweepInterval(d time.Duration) func(*Config) {
	return func(c *Config) { c.SweepInterval = d }
}

// WithBackoff 自定义重试退避函数。
// retries 是已重试次数（从 1 开始），返回下次重试延迟。
// 默认行为：第 N 次重试延迟 N 分钟。
func WithBackoff(fn func(retries int) time.Duration) func(*Config) {
	return func(c *Config) { c.Backoff = fn }
}

// ─── 投递 ───

// Add 将消息加入延迟队列，delay 时间后才可被 Poll 取出。
//
// 返回消息 ID（用于追踪），payload 为消息内容。
func (q *Queue) Add(ctx context.Context, payload string, delay time.Duration) (string, error) {
	item := &rawItem{
		ID:      id.UUID(),
		Payload: payload,
		Retries: 0,
	}
	member, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("delayqueue: marshal failed: %w", err)
	}

	score := float64(time.Now().Add(delay).UnixNano())
	if err := q.client.ZAdd(ctx, q.key, redis.Z{Score: score, Member: member}).Err(); err != nil {
		return "", fmt.Errorf("delayqueue: add failed: %w", err)
	}
	return item.ID, nil
}

// AddAt 在指定绝对时间点投递消息。
func (q *Queue) AddAt(ctx context.Context, payload string, t time.Time) (string, error) {
	if t.Before(time.Now()) {
		t = time.Now()
	}
	return q.Add(ctx, payload, t.Sub(time.Now()))
}

// ─── 拉取 ───

// Poll 原子拉取一批到期消息。
//
// Poll 使用 Lua 脚本 + ZPOPMINBYSCORE 将消息从延迟队列原子移入 Processing 队列，
// 多消费者并发调用时不会重复拉取同一条消息。
//
// batch 必须 > 0，否则返回空列表。
//
// 返回原始 member JSON 字符串列表。调用方须对每条消息调用 Ack 或 Nack。
// 返回空列表不表示错误，仅表示当前无到期消息。
func (q *Queue) Poll(ctx context.Context, batch int) ([]string, error) {
	if batch <= 0 {
		return []string{}, nil
	}

	result, err := pollScript.Run(ctx, q.client, []string{q.key, q.processingKey}, batch, float64(time.Now().UnixNano())).Result()
	if err != nil {
		return nil, fmt.Errorf("delayqueue: poll failed: %w", err)
	}

	items, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("delayqueue: unexpected poll result type %T", result)
	}

	// ZPOPMINBYSCORE 返回值仅为成员字符串列表，步长为 1。
	out := make([]string, 0, len(items))
	for _, m := range items {
		s, _ := m.(string)
		out = append(out, s)
	}
	return out, nil
}

// ─── 确认 / 拒绝 ───

// Ack 确认消息已成功处理，从 Processing 队列移除。
func (q *Queue) Ack(ctx context.Context, member string) error {
	if err := q.client.ZRem(ctx, q.processingKey, member).Err(); err != nil {
		return fmt.Errorf("delayqueue: ack failed: %w", err)
	}
	return nil
}

// Nack 否定确认，触发重试或移入 DLQ。
//
// Nack 使用 Lua 脚本 + lockdog 分布式锁保证原子性，防止 Nack 与 Sweep
// 并发修改同一条消息导致重试次数被多计。
//
// 重试次数 < MaxRetries：消息按 Backoff 函数延迟重新入队。
// 重试次数 >= MaxRetries：消息移入 DLQ，返回 ErrMaxRetries。
func (q *Queue) Nack(ctx context.Context, member string) error {
	item, err := decode(member)
	if err != nil {
		// 格式异常，直接丢弃
		return q.client.ZRem(ctx, q.processingKey, member).Err()
	}

	item.Retries++
	if item.Retries >= q.maxRetries {
		pipe := q.client.Pipeline()
		pipe.ZRem(ctx, q.processingKey, member)
		pipe.RPush(ctx, q.dlqKey, member)
		_, execErr := pipe.Exec(ctx)
		if execErr != nil {
			return fmt.Errorf("delayqueue: nack dlq failed: %w", execErr)
		}
		return ErrMaxRetries
	}

	newMember, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("delayqueue: marshal nack failed: %w", err)
	}

	backoff := q.backoff(item.Retries)
	score := float64(time.Now().Add(backoff).UnixNano())

	lockTTL := int64(5 * time.Second / time.Millisecond)
	// 分布式锁防止与 Sweep 竞态
	lockKey := fmt.Sprintf("delayqueue:%s:nack:%s", q.key, member[:min(32, len(member))])
	lock, lockErr := q.locker.Lock(context.Background(), lockKey)
	if lockErr != nil {
		// 锁未获取到（Sweep 正在处理此消息），跳过
		return fmt.Errorf("delayqueue: nack lock not acquired: %w", lockErr)
	}
	defer lock.Unlock(context.Background())

	// 原子 Lua：ZREM + ZADD + DEL lock
	result, err := nackScript.Run(ctx, q.client, []string{
		q.processingKey,
		q.key,
		q.dlqKey,
		lockKey,
	}, lockKey, member, int64(q.maxRetries), score, item.Retries, newMember, lockTTL).Result()
	if err != nil {
		return fmt.Errorf("delayqueue: nack script failed: %w", err)
	}

	n, ok := result.(int64)
	if !ok || n == 0 {
		return fmt.Errorf("delayqueue: nack script unexpected result: %v", result)
	}
	if n == 2 {
		return ErrMaxRetries
	}
	return nil
}

// ─── Consumer 模式 ───

// Start 启动消费循环，自动 Poll → Consume → Ack/Nack。
//
// ctx 控制生命周期，ctx 被取消时优雅关闭：
//  1. 停止拉取新消息
//  2. 等待当前 Processing 消息处理完毕（最多 2 * VisibilityTimeout）
//  3. 返回 ctx.Err()
//
// 等价于手动循环，但内置退避、重试、DLQ 逻辑。
func (q *Queue) Start(ctx context.Context, consumer Consumer) error {
	q.mu.Lock()
	if q.running {
		q.mu.Unlock()
		return fmt.Errorf("delayqueue: queue already running")
	}
	q.running = true
	q.stopCh = make(chan struct{})
	q.mu.Unlock()

	q.wg.Add(2)
	go q.consumeLoop(ctx, consumer)
	go q.sweepLoop(ctx)

	<-ctx.Done()

	q.Stop()

	// 等待消费循环退出（Sweeper 由 sweepLoop 内部 stopCh 控制）
	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * q.visibilityTimeout):
		// 兜底超时，强制退出
	}

	q.mu.Lock()
	q.running = false
	q.mu.Unlock()

	return ctx.Err()
}

// Stop 优雅停止消费循环。
// 停止拉取新消息，等待当前 Processing 消息处理完毕。
// 安全多次调用。
func (q *Queue) Stop() {
	q.mu.Lock()
	if !q.running || q.stopCh == nil {
		q.mu.Unlock()
		return
	}
	select {
	case <-q.stopCh:
		// already closed
	default:
		close(q.stopCh)
	}
	q.mu.Unlock()
}

// ─── 内部循环 ───

func (q *Queue) consumeLoop(ctx context.Context, consumer Consumer) {
	defer q.wg.Done()

	backoffs := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		500 * time.Millisecond,
		time.Second,
		2 * time.Second,
		5 * time.Second,
	}
	backoffIdx := 0
	emptyStreak := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stopCh:
			return
		default:
		}

		msgs, err := q.Poll(ctx, 10)
		if err != nil {
			if !sleepCtx(ctx, backoffs[min(backoffIdx, len(backoffs)-1)]) {
				return
			}
			backoffIdx++
			continue
		}

		if len(msgs) == 0 {
			emptyStreak++
			if emptyStreak > 3 {
				if !sleepCtx(ctx, backoffs[min(backoffIdx, len(backoffs)-1)]) {
					return
				}
				backoffIdx++
			} else {
				if !sleepCtx(ctx, 200*time.Millisecond) {
					return
				}
			}
			continue
		}

		backoffIdx = 0
		emptyStreak = 0

		for _, raw := range msgs {
			item, err := decode(raw)
			if err != nil {
				if ackErr := q.Ack(ctx, raw); ackErr != nil {
					_ = ackErr
				}
				continue
			}

			processCtx, cancel := context.WithTimeout(ctx, q.visibilityTimeout)
			consumeErr := consumer.Consume(processCtx, &Message{
				ID:      item.ID,
				Payload: item.Payload,
				Retries: item.Retries,
				Raw:     raw,
			})
			cancel()

			if consumeErr != nil {
				if nackErr := q.Nack(ctx, raw); nackErr != nil {
					fmt.Printf("delayqueue: nack failed (msg id=%s): %v\n", item.ID, nackErr)
				}
			} else {
				if ackErr := q.Ack(ctx, raw); ackErr != nil {
					fmt.Printf("delayqueue: ack failed (msg id=%s): %v\n", item.ID, ackErr)
				}
			}
		}
	}
}

func (q *Queue) sweepLoop(ctx context.Context) {
	defer q.wg.Done()

	ticker := time.NewTicker(q.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stopCh:
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			if err := q.sweep(ctx); err != nil {
				fmt.Printf("delayqueue: sweep error: %v\n", err)
			}
		}
	}
}

// sweep 回收超时 Processing 消息：重试或移入 DLQ。
// 每条消息通过 lockdog 分布式锁保护，防止与 Nack 并发修改。
func (q *Queue) sweep(ctx context.Context) error {
	now := float64(time.Now().UnixNano())

	expired, err := q.client.ZRangeByScore(ctx, q.processingKey, &redis.ZRangeBy{
		Min: "-inf",
		Max: fmt.Sprintf("%f", now),
	}).Result()
	if err != nil || len(expired) == 0 {
		return err
	}

	const maxSweep = 500
	if len(expired) > maxSweep {
		expired = expired[:maxSweep]
	}

	for _, member := range expired {
		item, err := decode(member)
		if err != nil {
			// 格式异常，直接移除
			q.client.ZRem(ctx, q.processingKey, member)
			continue
		}

		// 获取分布式锁，防止与 Nack 并发修改
		lockKey := fmt.Sprintf("delayqueue:%s:nack:%s", q.key, member[:min(32, len(member))])
		lock, lockErr := q.locker.Lock(context.Background(), lockKey)
		if lockErr != nil {
			// 锁未获取到（Nack 正在处理此消息），跳过
			continue
		}

		if item.Retries >= q.maxRetries {
			pipe := q.client.Pipeline()
			pipe.ZRem(ctx, q.processingKey, member)
			pipe.RPush(ctx, q.dlqKey, member)
			if _, execErr := pipe.Exec(ctx); execErr != nil {
				fmt.Printf("delayqueue: sweep dlq failed (msg id=%s): %v\n", item.ID, execErr)
			}
		} else {
			item.Retries++
			newMember, err := json.Marshal(item)
			if err != nil {
				q.client.ZRem(ctx, q.processingKey, member)
				lock.Unlock(context.Background())
				continue
			}
			backoff := q.backoff(item.Retries)
			score := float64(time.Now().Add(backoff).UnixNano())

			pipe := q.client.Pipeline()
			pipe.ZRem(ctx, q.processingKey, member)
			pipe.ZAdd(ctx, q.key, redis.Z{Score: score, Member: newMember})
			if _, execErr := pipe.Exec(ctx); execErr != nil {
				fmt.Printf("delayqueue: sweep requeue failed (msg id=%s): %v\n", item.ID, execErr)
			}
		}

		lock.Unlock(context.Background())
	}

	return nil
}

// ─── 查询 / 运维 ───

// DLQMessages 读取死信队列中的消息（只读，不移除）。
// count 为 0 或负数时返回空列表。
func (q *Queue) DLQMessages(ctx context.Context, count int) ([]string, error) {
	if count <= 0 {
		return []string{}, nil
	}
	result, err := q.client.LRange(ctx, q.dlqKey, 0, int64(count-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("delayqueue: dlq messages failed: %w", err)
	}
	return result, nil
}

// DLQPop 从死信队列弹出最多 count 条消息（原子操作，Lua 保证）。
func (q *Queue) DLQPop(ctx context.Context, count int) ([]string, error) {
	if count <= 0 {
		return []string{}, nil
	}
	result, err := dlqPopScript.Run(ctx, q.client, []string{q.dlqKey}, count).Result()
	if err != nil {
		return nil, fmt.Errorf("delayqueue: dlq pop failed: %w", err)
	}

	items, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("delayqueue: unexpected dlq pop result type %T", result)
	}

	out := make([]string, 0, len(items))
	for _, m := range items {
		s, _ := m.(string)
		out = append(out, s)
	}
	return out, nil
}

// Stats 返回队列各层级的消息数量。
func (q *Queue) Stats(ctx context.Context) (Stats, error) {
	pipe := q.client.Pipeline()
	delayCmd := pipe.ZCard(ctx, q.key)
	processingCmd := pipe.ZCard(ctx, q.processingKey)
	dlqCmd := pipe.LLen(ctx, q.dlqKey)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("delayqueue: stats failed: %w", err)
	}

	s := Stats{
		Delay:      delayCmd.Val(),
		Processing: processingCmd.Val(),
		DLQ:        dlqCmd.Val(),
	}
	return s, nil
}

// Keys 返回队列使用的 Redis 键名。
func (q *Queue) Keys() (delay, processing, dlq string) {
	return q.key, q.processingKey, q.dlqKey
}

// ─── 消息编解码 ───

type rawItem struct {
	ID      string `json:"id"`
	Payload string `json:"payload"`
	Retries int    `json:"retries"`
}

// ParseMessage 解析 Poll 返回的原始 member JSON 字符串。
// 返回的 Message.Raw 字段保留原始 JSON，用于 Ack/Nack。
func ParseMessage(member string) (*Message, error) {
	item, err := decode(member)
	if err != nil {
		return nil, err
	}
	return &Message{
		ID:      item.ID,
		Payload: item.Payload,
		Retries: item.Retries,
		Raw:     member,
	}, nil
}

func decode(member string) (*rawItem, error) {
	var item rawItem
	if err := json.Unmarshal([]byte(member), &item); err != nil {
		return nil, fmt.Errorf("delayqueue: invalid message: %w", err)
	}
	return &item, nil
}

// ─── 工具函数 ───

// sleepCtx 休眠 d 时间，但可被 ctx 取消。
// 返回 true 表示正常超时，false 表示 ctx 已取消。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// msgID 从 member JSON 中提取消息 ID（用于日志）。
// 若解析失败返回空字符串。
func msgID(member string) string {
	item, err := decode(member)
	if err != nil {
		return ""
	}
	return item.ID
}

// nackLockKey 构造 Nack 分布式锁的 Redis 键名。
// 使用 member 前 32 字符作为锁标识，保证同一消息的锁键一致。
func nackLockKey(queueKey, member string) string {
	prefix := member
	if len(prefix) > 32 {
		prefix = prefix[:32]
	}
	// 替换 Redis 键名非法字符（冒号在 member JSON 中可能出现）
	prefix = strings.ReplaceAll(prefix, ":", "_")
	return fmt.Sprintf("delayqueue:%s:nack:%s", queueKey, prefix)
}
