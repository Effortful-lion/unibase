package lockdog

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func newTestClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	// 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		t.Skipf("Redis 不可用，跳过集成测试: %v", err)
	}
	t.Cleanup(func() {
		client.Close()
	})
	return client
}

func TestLockUnlock(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := "test:lock:basic"

	locker := New(client, WithOwner("test-worker"), WithTTL(5*time.Second), WithWatchdogInterval(1*time.Second))

	// 1. 第一次加锁成功
	lock1, err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("第一次加锁应成功: %v", err)
	}

	// 2. 第二次加锁应失败（锁已被持有）
	_, err = locker.Lock(ctx, key)
	if !errors.Is(err, ErrLockNotAcquired) {
		t.Fatalf("第二次加锁应失败，实际: %v", err)
	}

	// 3. 释放锁
	if err := lock1.Unlock(ctx); err != nil {
		t.Fatalf("释放锁失败: %v", err)
	}

	// 4. 释放后再次加锁应成功
	lock2, err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("释放后加锁应成功: %v", err)
	}
	lock2.Unlock(ctx)
}

func TestWatchdogRenew(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := "test:lock:watchdog"

	locker := New(client,
		WithOwner("test-worker"),
		WithTTL(2*time.Second),
		WithWatchdogInterval(500*time.Millisecond),
	)

	lock, err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("加锁失败: %v", err)
	}
	defer lock.Unlock(ctx)

	// 等待超过 TTL，看门狗应该已经续期
	time.Sleep(3 * time.Second)

	// 锁应该仍然有效（看门狗续期了）
	_, err = locker.Lock(ctx, key)
	if !errors.Is(err, ErrLockNotAcquired) {
		t.Fatalf("看门狗续期后锁应仍被持有，实际: %v", err)
	}
}

// TestLockReleaseAndReacquire 验证释放锁后其他持有者可以重新获取。
// 通过 worker-1 释放锁后 worker-2 加锁成功，验证 Lua 脚本安全释放的正确性。
func TestLockReleaseAndReacquire(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := "test:lock:release-reacquire"

	locker1 := New(client, WithOwner("worker-1"), WithTTL(5*time.Second))
	locker2 := New(client, WithOwner("worker-2"), WithTTL(5*time.Second))

	// worker-1 加锁
	lock1, err := locker1.Lock(ctx, key)
	if err != nil {
		t.Fatalf("worker-1 加锁失败: %v", err)
	}

	// worker-2 此时无法获取同一把锁
	_, err = locker2.Lock(ctx, key)
	if !errors.Is(err, ErrLockNotAcquired) {
		t.Fatalf("worker-1 持锁时 worker-2 应获取失败，实际: %v", err)
	}

	// worker-1 释放锁
	if err := lock1.Unlock(ctx); err != nil {
		t.Fatalf("worker-1 释放锁应成功: %v", err)
	}

	// 释放后 worker-2 可以正常加锁
	lock2, err := locker2.Lock(ctx, key)
	if err != nil {
		t.Fatalf("释放后 worker-2 加锁应成功: %v", err)
	}
	defer lock2.Unlock(ctx)
}

func TestKeyFormat(t *testing.T) {
	got := lockKey("order-1001")
	want := "lock:order-1001"
	if got != want {
		t.Errorf("key 格式错误: got %q, want %q", got, want)
	}
}

func TestRandomToken(t *testing.T) {
	token1, err := randomToken()
	if err != nil {
		t.Fatalf("生成 token 失败: %v", err)
	}
	token2, err := randomToken()
	if err != nil {
		t.Fatalf("生成 token 失败: %v", err)
	}
	if token1 == token2 {
		t.Error("两次生成的 token 应不同")
	}
	if len(token1) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("token 长度应为 32，实际: %d", len(token1))
	}
}

func TestWatchdogDisabled(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := "test:lock:no-watchdog"

	// 禁用看门狗
	locker := New(client, WithOwner("test"), WithTTL(1*time.Second), WithWatchdogInterval(0))

	lock, err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("加锁失败: %v", err)
	}
	defer lock.Unlock(ctx)

	// 等待 TTL 过期
	time.Sleep(2 * time.Second)

	// 先清理可能残留的 key（Redis 惰性过期可能还没触发）
	lock.Unlock(ctx)

	// 过期后应能重新获取锁
	lock2, err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("过期后应能重新获取锁: %v", err)
	}
	defer lock2.Unlock(ctx)
}

func TestDoubleUnlock(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := "test:lock:double"

	locker := New(client, WithOwner("test"), WithTTL(5*time.Second))
	lock, err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("加锁失败: %v", err)
	}

	// 第一次释放成功
	if err := lock.Unlock(ctx); err != nil {
		t.Fatalf("第一次释放失败: %v", err)
	}

	// 第二次释放应返回 ErrLockNotHeld（锁已释放）
	err = lock.Unlock(ctx)
	if !errors.Is(err, ErrLockNotHeld) {
		t.Fatalf("二次释放应返回 ErrLockNotHeld，实际: %v", err)
	}
}

func TestLostChannel(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := "test:lock:lost"

	locker := New(client,
		WithOwner("thief"),
		WithTTL(10*time.Second),
		WithWatchdogInterval(200*time.Millisecond),
	)

	lock, err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("加锁失败: %v", err)
	}
	defer lock.Unlock(ctx)

	// 用另一个 token 覆盖 Redis key，模拟锁被他人抢占
	thiefToken := "thief-token-xxx"
	if err := client.Set(ctx, lockKey(key), thiefToken, 10*time.Second).Err(); err != nil {
		t.Fatalf("模拟锁被抢占失败: %v", err)
	}

	// 等待看门狗检测到 token 不匹配
	lostCh := lock.Lost()
	select {
	case <-lostCh:
		// 预期：Lost channel 被关闭
	case <-time.After(2 * time.Second):
		t.Fatal("看门狗未检测到锁丢失，Lost channel 未关闭")
	}

	// Unlock 应返回 ErrLockLost
	err = lock.Unlock(ctx)
	if !errors.Is(err, ErrLockLost) {
		t.Fatalf("锁丢失后 Unlock 应返回 ErrLockLost，实际: %v", err)
	}

	// 清理：用正确的 token 删除被抢占的 key
	client.Eval(ctx, unlockLua, []string{lockKey(key)}, thiefToken)
}

func TestWatchdogDisabledLostChannel(t *testing.T) {
	// 未启用看门狗时，Lost() 应返回 nil
	client := newTestClient(t)
	ctx := context.Background()
	key := "test:lock:no-watchdog-lost"

	locker := New(client, WithOwner("test"), WithTTL(5*time.Second), WithWatchdogInterval(0))
	lock, err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("加锁失败: %v", err)
	}
	defer lock.Unlock(ctx)

	if lock.Lost() != nil {
		t.Error("未启用看门狗时 Lost() 应返回 nil")
	}
}

func TestOptionsValidation(t *testing.T) {
	// WithTTL(0) 应忽略，保持默认值
	opts1 := options{ttl: DefaultTTL}
	WithTTL(0)(&opts1)
	if opts1.ttl != DefaultTTL {
		t.Errorf("WithTTL(0) 应保持默认值，实际: %v", opts1.ttl)
	}

	// WithTTL(负数) 应忽略
	opts2 := options{ttl: DefaultTTL}
	WithTTL(-1 * time.Second)(&opts2)
	if opts2.ttl != DefaultTTL {
		t.Errorf("WithTTL(负数) 应保持默认值，实际: %v", opts2.ttl)
	}

	// WithWatchdogInterval(负数) 应设为 0（禁用）
	opts3 := options{watchdog: DefaultWatchdogInterval}
	WithWatchdogInterval(-1 * time.Second)(&opts3)
	if opts3.watchdog != 0 {
		t.Errorf("WithWatchdogInterval(负数) 应设为 0，实际: %v", opts3.watchdog)
	}
}

func Example() {
	// 创建分布式锁实例
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	locker := New(client,
		WithOwner("my-service"),
		WithTTL(15*time.Second),
		WithWatchdogInterval(5*time.Second),
	)

	// 获取锁
	ctx := context.Background()
	lock, err := locker.Lock(ctx, "task:order-1001")
	if err != nil {
		// 锁已被他人持有
		return
	}
	defer lock.Unlock(ctx)

	// 执行临界区代码
	fmt.Println("critical section")
}
