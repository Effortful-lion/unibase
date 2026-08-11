package lockdog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultTTL 是默认锁过期时间。
	DefaultTTL = 15 * time.Second
	// DefaultWatchdogInterval 是默认看门狗续期间隔。
	DefaultWatchdogInterval = 5 * time.Second
)

// unlockLua 是安全释放锁的 Lua 脚本。
// 只有 value 匹配时才删除，防止误删他人锁。
const unlockLua = `
local key = KEYS[1]
local token = ARGV[1]
local current = redis.call('get', key)
if current == token then
    return redis.call('del', key)
end
return 0
`

// renewLua 是安全续期锁的 Lua 脚本。
// 只有 value 匹配时才续期，防止给不持有的锁续期。
// 使用 pexpire 以毫秒精度续期，避免亚秒级 TTL 被截断。
const renewLua = `
local key = KEYS[1]
local token = ARGV[1]
local ttl = ARGV[2]
local current = redis.call('get', key)
if current == token then
    return redis.call('pexpire', key, ttl)
end
return 0
`

// Lock 表示一把已持有的分布式锁。
type Lock interface {
	// Unlock 释放锁。必须在临界区结束后调用，通常配合 defer 使用。
	Unlock(ctx context.Context) error
	// Lost 返回一个 channel，当锁在持有期间被其他进程抢占时关闭。
	// 调用方可在 select 中监听该 channel，提前终止临界区操作。
	// 未启用看门狗时返回 nil。
	Lost() <-chan struct{}
}

// Locker 是分布式锁的获取入口。
type Locker interface {
	Lock(ctx context.Context, key string, opts ...Option) (Lock, error)
}

// redisLock 是 Locker 的具体实现。
type redisLock struct {
	client       redis.Cmdable
	ttl          time.Duration
	watchdog     time.Duration
	owner        string // 看门狗的所有者标识，用于日志
	onRenewError func(key, owner string, err error)
}

// New 创建一个基于 Redis 的分布式锁实例。
func New(client redis.Cmdable, opts ...Option) Locker {
	cfg := defaultOptions()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &redisLock{
		client:       client,
		ttl:          cfg.ttl,
		watchdog:     cfg.watchdog,
		owner:        cfg.owner,
		onRenewError: cfg.onRenewError,
	}
}

// Lock 尝试获取锁。
//
// 加锁成功返回 Lock 实例，调用方必须调用 Unlock 释放。
// 如果锁已被他人持有，返回 ErrLockNotAcquired。
//
// 注意：ctx 的生命周期决定了看门狗的存活时间。如果 ctx 被取消或超时，
// 看门狗将停止续期，锁会在 TTL 后自动过期。确保传入的 ctx 覆盖整个
// 临界区执行周期，或使用 context.Background()。
func (l *redisLock) Lock(ctx context.Context, key string, opts ...Option) (Lock, error) {
	cfg := options{
		ttl:          l.ttl,
		watchdog:     l.watchdog,
		owner:        l.owner,
		onRenewError: l.onRenewError,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	token, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	// SET key token NX EX ttl
	ok, err := l.client.SetNX(ctx, lockKey(key), token, cfg.ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !ok {
		return nil, ErrLockNotAcquired
	}

	inner := &innerLock{
		client:       l.client,
		key:          lockKey(key),
		token:        token,
		ttl:          cfg.ttl,
		watchdog:     cfg.watchdog,
		owner:        cfg.owner,
		onRenewError: cfg.onRenewError,
	}

	// 如果配置了看门狗，启动续期 goroutine
	if cfg.watchdog > 0 {
		inner.startWatchdog(ctx)
	}

	return inner, nil
}

// innerLock 是已持有的锁实例。
type innerLock struct {
	client       redis.Cmdable
	key          string
	token        string
	ttl          time.Duration
	watchdog     time.Duration
	owner        string
	mu           sync.Mutex
	cancel       context.CancelFunc
	renewDone    chan struct{} // 看门狗退出信号
	lostCh       chan struct{} // 锁丢失信号
	lost         atomic.Bool   // 标记锁是否已丢失，防止重复关闭 lostCh
	onRenewError func(key, owner string, err error)
}

func (l *innerLock) startWatchdog(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	l.cancel = cancel
	l.renewDone = make(chan struct{})
	l.lostCh = make(chan struct{})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[lockdog] watchdog panic recovered: key=%s owner=%s panic=%v\n", l.key, l.owner, r)
			}
			close(l.renewDone)
		}()
		ticker := time.NewTicker(l.watchdog)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// 使用 Lua 脚本安全续期，只有 token 匹配时才续期
				// 使用毫秒精度避免亚秒级 TTL 被截断为 0
				ttlMillis := l.ttl.Milliseconds()
				result, err := l.client.Eval(ctx, renewLua, []string{l.key}, l.token, ttlMillis).Result()
				if err != nil {
					if l.onRenewError != nil {
						l.onRenewError(l.key, l.owner, err)
					}
					continue
				}
				// 返回值 0 表示 token 不匹配，锁已被他人获取，通知调用方并停止续期
				// 注意：此处不能获取 l.mu，否则与 Unlock 持锁等待 renewDone 时互相死锁。
				// lost 标志使用 atomic 保证并发安全。
				if n, ok := result.(int64); ok && n == 0 {
					if l.lost.CompareAndSwap(false, true) {
						close(l.lostCh)
					}
					cancel()
				}
			}
		}
	}()
}

// Unlock 释放锁。
//
// 释放流程：
//  1. 停止看门狗
//  2. 通过 Lua 脚本原子性删除（只有 token 匹配才删）
func (l *innerLock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 如果锁已被他人抢占，返回 ErrLockLost
	if l.lost.Load() {
		return ErrLockLost
	}

	// 停止看门狗
	if l.cancel != nil {
		l.cancel()
		// 等待看门狗 goroutine 退出，避免竞争
		<-l.renewDone
	}

	// Lua 脚本安全释放
	result, err := l.client.Eval(ctx, unlockLua, []string{l.key}, l.token).Result()
	if err != nil {
		return fmt.Errorf("unlock: %w", err)
	}

	// result 为 1 表示成功释放，0 表示锁已不存在或不属于当前持有者
	if n, ok := result.(int64); ok && n == 1 {
		return nil
	}
	return ErrLockNotHeld
}

// Lost 返回一个 channel，当锁被其他进程抢占时关闭。
// 调用方可在 select 中监听该 channel，提前终止临界区操作。
// 未启用看门狗时返回 nil。
func (l *innerLock) Lost() <-chan struct{} {
	return l.lostCh
}

func lockKey(key string) string {
	return fmt.Sprintf("lock:%s", key)
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
