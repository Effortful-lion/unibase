/*
Package lockdog 提供基于 Redis 的分布式锁能力。

核心特性：

  - SET key value NX EX ttl：原子性加锁
  - 看门狗（watchdog）：后台 goroutine 定期续期，防止执行过程中锁过期
  - Lua 安全释放：只有锁持有者才能释放，避免误删他人锁

使用方式：

	locker := lockdog.New(redisClient)
	lock, err := locker.Lock(ctx, "task:order-1001")
	if err != nil {
	    // 被别人抢了
	    return err
	}
	defer lock.Unlock(ctx)

	// 执行临界区代码
	doSomething()
*/
package lockdog
