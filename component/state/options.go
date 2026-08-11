package state

import (
	"context"
	"time"

	"github.com/Effortful-lion/unibase/component/lockdog"
	"github.com/Effortful-lion/unibase/logx"
)

// SubTaskFailStrategy 定义子任务失败时的处理策略。
type SubTaskFailStrategy int

const (
	// SubTaskFailStop 遇到子任务失败立即停止整个流程。
	SubTaskFailStop SubTaskFailStrategy = iota
	// SubTaskFailContinue 子任务失败后继续执行剩余子任务，最终标记为失败。
	SubTaskFailContinue
)

// RetryPolicy 定义重试策略。
type RetryPolicy struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	BackoffRate     float64
}

// options 管理器的运行配置。
type options struct {
	description         string
	taskLoader          any
	subTaskFailStrategy SubTaskFailStrategy
	retryPolicy         *RetryPolicy
	enableLock          bool
	locker              Locker
	lockTimeout         time.Duration
	logger              *logx.Logger
}

func defaultOptions() options {
	return options{
		subTaskFailStrategy: SubTaskFailStop,
		logger:              logx.Module("state"),
	}
}

// Option 配置管理器的可选参数。
type Option func(*options)

// WithDescription 设置管理器的描述信息。
func WithDescription(description string) Option {
	return func(o *options) {
		o.description = description
	}
}

// WithTaskLoader 设置任务加载器，用于断点恢复时加载主任务和子任务。
func WithTaskLoader(loader any) Option {
	return func(o *options) {
		o.taskLoader = loader
	}
}

// WithSubTaskFailStrategy 设置子任务失败策略。
// 默认 SubTaskFailStop：遇到失败立即停止。
func WithSubTaskFailStrategy(strategy SubTaskFailStrategy) Option {
	return func(o *options) {
		o.subTaskFailStrategy = strategy
	}
}

// WithRetryPolicy 设置全局重试策略。
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(o *options) {
		o.retryPolicy = &policy
	}
}

// WithLocker 设置任务锁，防止同一条任务并发执行。
func WithLocker(locker Locker) Option {
	return func(o *options) {
		o.locker = locker
		o.enableLock = true
		if o.lockTimeout == 0 {
			o.lockTimeout = 30 * time.Second
		}
	}
}

// WithLockTimeout 设置获取分布式锁的超时时间。
// 仅在启用 WithLocker 时生效，默认 30s。
func WithLockTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.lockTimeout = timeout
	}
}

// WithLogger 设置自定义日志器。
func WithLogger(logger *logx.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// Locker 是任务锁的统一接口。
type Locker interface {
	Lock(ctx context.Context, taskID string) (taskLock, error)
}

// taskLock 是状态机内部使用的锁抽象。
type taskLock interface {
	Unlock(ctx context.Context) error
}

// lockdogLocker 基于 lockdog 的分布式锁实现。
type lockdogLocker struct {
	factory func() lockdog.Locker
	opts    []lockdog.Option
}

// NewLocker 创建一个基于 lockdog 的任务锁。
// lockerFactory 返回 lockdog.Locker 实例，用于按 taskID 获取分布式锁。
// 单进程单实例部署不需要锁，直接省略 WithLocker 选项即可。
func NewLocker(lockerFactory func() lockdog.Locker, opts ...lockdog.Option) Locker {
	return &lockdogLocker{
		factory: lockerFactory,
		opts:    opts,
	}
}

func (l *lockdogLocker) Lock(ctx context.Context, taskID string) (taskLock, error) {
	if l.factory == nil {
		return nil, &StateError{message: "locker factory is nil"}
	}
	lock, err := l.factory().Lock(ctx, taskID, l.opts...)
	if err != nil {
		return nil, err
	}
	return &lockdogTaskLock{lock: lock}, nil
}

// lockdogTaskLock 将 lockdog.Lock 适配为内部 taskLock 接口。
type lockdogTaskLock struct {
	lock lockdog.Lock
}

func (l *lockdogTaskLock) Unlock(ctx context.Context) error {
	return l.lock.Unlock(ctx)
}

// noopLocker 是空锁实现，用于单进程无锁场景。
type noopLocker struct{}

func (n *noopLocker) Lock(ctx context.Context, taskID string) (taskLock, error) {
	return &noopTaskLock{}, nil
}

// noopTaskLock 空锁实现。
type noopTaskLock struct{}

func (n *noopTaskLock) Unlock(ctx context.Context) error { return nil }
