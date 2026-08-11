package lockdog

import (
	"fmt"
	"time"
)

// Option 配置锁的行为。
type Option func(*options)

type options struct {
	ttl          time.Duration
	watchdog     time.Duration
	owner        string
	onRenewError func(key, owner string, err error)
}

func defaultOptions() options {
	return options{
		ttl:      DefaultTTL,
		watchdog: DefaultWatchdogInterval,
		owner:    "unknown",
		onRenewError: func(key, owner string, err error) {
			fmt.Printf("[lockdog] watchdog renew failed: key=%s owner=%s err=%v\n", key, owner, err)
		},
	}
}

// WithTTL 设置锁的过期时间。
// 默认 15s。建议大于 watchdog 间隔的 2 倍，防止续期失败时锁提前过期。
// ttl 必须大于 0，否则保持默认值。
func WithTTL(ttl time.Duration) Option {
	return func(o *options) {
		if ttl > 0 {
			o.ttl = ttl
		}
	}
}

// WithWatchdogInterval 设置看门狗续期间隔。
// 默认 5s。传 0 表示禁用看门狗，锁过期后不会续期。
// interval 为负值时视为禁用看门狗。
func WithWatchdogInterval(interval time.Duration) Option {
	return func(o *options) {
		if interval > 0 {
			o.watchdog = interval
		} else {
			o.watchdog = 0
		}
	}
}

// WithOwner 设置锁持有者的标识，用于日志和调试。
func WithOwner(owner string) Option {
	return func(o *options) {
		o.owner = owner
	}
}

// WithOnRenewError 设置看门狗续期失败时的回调函数。
// 允许调用方自定义错误日志的输出方式和目标。传 nil 表示忽略续期错误。
func WithOnRenewError(handler func(key, owner string, err error)) Option {
	return func(o *options) {
		o.onRenewError = handler
	}
}
