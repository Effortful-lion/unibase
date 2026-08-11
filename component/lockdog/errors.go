package lockdog

import "errors"

var (
	// ErrLockNotAcquired 表示锁已被他人持有，获取失败。
	ErrLockNotAcquired = errors.New("lock not acquired")
	// ErrLockNotHeld 表示当前实例不持有该锁，无法释放。
	ErrLockNotHeld = errors.New("lock not held")
	// ErrLockLost 表示锁在持有期间被其他进程获取（TTL 过期后未及时续期）。
	ErrLockLost = errors.New("lock lost")
)
