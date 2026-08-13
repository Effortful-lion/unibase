package cluster

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ── Redis 集成测试 ──────────────────────────────────────────────
// 本地运行：REDIS_ADDR=localhost:6379 go test ./internal/cluster/ -run TestRedisDiscovery
// CI 中：go test -short ./internal/cluster/ （跳过集成测试）

const redisStartTimeout = 60 * time.Second
const redisAddrEnv = "REDIS_ADDR"

// newRedisDiscovery 创建 RedisDiscovery 连接。
// 优先使用 REDIS_ADDR 环境变量（本地 Redis），否则启动 testcontainer。
func newRedisDiscovery(t *testing.T, prefix string) (*RedisDiscovery, string, func()) {
	t.Helper()

	// 优先使用环境变量指定的 Redis 地址
	if addr := os.Getenv(redisAddrEnv); addr != "" {
		return newRedisDiscoveryWithAddr(t, prefix, addr)
	}

	// 回退到 testcontainer
	return newRedisDiscoveryWithContainer(t, prefix)
}

// newRedisDiscoveryWithAddr 连接到已有的 Redis 实例。
func newRedisDiscoveryWithAddr(t *testing.T, prefix string, addr string) (*RedisDiscovery, string, func()) {
	t.Helper()

	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
		MinIdleConns: 2,
	})

	// 验证连接
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping %s: %v", addr, err)
	}

	if prefix == "" {
		prefix = "test:cluster"
	}
	discovery := NewRedisDiscovery(rdb, prefix)

	cleanup := func() {
		_ = rdb.Close()
	}

	t.Cleanup(func() {
		// 清理测试数据
		_ = rdb.FlushDB(ctx).Err()
		_ = rdb.Close()
	})

	return discovery, addr, cleanup
}

// newRedisDiscoveryWithContainer 启动 Redis 容器并连接。
func newRedisDiscoveryWithContainer(t *testing.T, prefix string) (*RedisDiscovery, string, func()) {
	t.Helper()

	ctx := context.Background()

	// 启动 Redis 容器
	container, err := testcontainers.Run(ctx, "redis:7-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").
				WithStartupTimeout(redisStartTimeout),
		),
	)
	if err != nil {
		t.Fatalf("failed to start redis container: %v", err)
	}

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("failed to get container endpoint: %v", err)
	}

	// 创建 Redis 客户端
	rdb := redis.NewClient(&redis.Options{
		Addr:         endpoint,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
		MinIdleConns: 2,
	})

	// 验证连接
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("failed to ping redis: %v", err)
	}

	if prefix == "" {
		prefix = "test:cluster"
	}
	discovery := NewRedisDiscovery(rdb, prefix)

	cleanup := func() {
		_ = rdb.Close()
		_ = container.Terminate(ctx)
	}

	t.Cleanup(cleanup)

	return discovery, endpoint, cleanup
}

// node 创建测试用 ClusterNode。
func node(tag, group string, role Role) ClusterNode {
	return ClusterNode{
		Tag:         tag,
		ServiceName: "svc",
		Group:       group,
		Role:        role,
		IPPort:      ":8080",
		ConnectUrl:  "http://:8080",
		Ts:          time.Now().Unix(),
	}
}

// ── Register + PullNodes 生命周期 ──────────────────────────────

func TestRedisDiscovery_RegisterAndPull(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	discovery, _, cleanup := newRedisDiscovery(t, "test:register")
	defer cleanup()

	ctx := context.Background()

	// 注册两个节点
	if err := discovery.Register(ctx, node("node-1", "g1", RoleAP)); err != nil {
		t.Fatalf("Register node-1: %v", err)
	}
	if err := discovery.Register(ctx, node("node-2", "g1", RoleAP)); err != nil {
		t.Fatalf("Register node-2: %v", err)
	}

	// 拉取节点
	nodes, err := discovery.PullNodes(ctx, "g1", RoleAP, 15*time.Minute)
	if err != nil {
		t.Fatalf("PullNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	tags := make(map[string]bool)
	for _, n := range nodes {
		tags[n.Tag] = true
	}
	if !tags["node-1"] {
		t.Error("node-1 not found in pulled nodes")
	}
	if !tags["node-2"] {
		t.Error("node-2 not found in pulled nodes")
	}
}

// ── Unregister 注销节点 ────────────────────────────────────────

func TestRedisDiscovery_Unregister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	discovery, _, cleanup := newRedisDiscovery(t, "test:unregister")
	defer cleanup()

	ctx := context.Background()

	// 注册后注销
	if err := discovery.Register(ctx, node("node-1", "g1", RoleAP)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := discovery.Unregister(ctx, node("node-1", "g1", RoleAP)); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	// 拉取应为空
	nodes, err := discovery.PullNodes(ctx, "g1", RoleAP, 15*time.Minute)
	if err != nil {
		t.Fatalf("PullNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes after unregister, got %d", len(nodes))
	}
}

// ── TTL 过期节点过滤 ───────────────────────────────────────────

func TestRedisDiscovery_PullNodes_ExpiredFiltered(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	discovery, endpoint, cleanup := newRedisDiscovery(t, "test:ttl")
	defer cleanup()

	ctx := context.Background()

	// 注册一个节点，然后手动将 ZSet score 设为 2 分钟前（模拟过期）
	staleNode := node("stale", "g1", RoleAP)
	if err := discovery.Register(ctx, staleNode); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Register 会覆盖 Ts，所以需要用独立客户端操作底层 ZSet score
	rdb := redis.NewClient(&redis.Options{
		Addr:         endpoint,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
		MinIdleConns: 2,
	})
	defer rdb.Close()

	zsetKey := "test:ttl:" + RoleAP.String() + ":g1"
	staleScore := time.Now().Add(-2 * time.Minute).Unix()
	if err := rdb.ZAdd(ctx, zsetKey, redis.Z{
		Score:  float64(staleScore),
		Member: "stale",
	}).Err(); err != nil {
		t.Fatalf("ZAdd (set stale score): %v", err)
	}

	// 用 1 分钟 TTL 拉取，过期节点应被清理
	nodes, err := discovery.PullNodes(ctx, "g1", RoleAP, 1*time.Minute)
	if err != nil {
		t.Fatalf("PullNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes (stale filtered by TTL), got %d", len(nodes))
	}
}

// ── 不同 group/role 隔离 ──────────────────────────────────────

func TestRedisDiscovery_GroupRoleIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	discovery, _, cleanup := newRedisDiscovery(t, "test:isolation")
	defer cleanup()

	ctx := context.Background()

	// 注册不同 group 和 role 的节点
	for _, n := range []ClusterNode{
		node("ap-1", "g1", RoleAP),
		node("ap-2", "g2", RoleAP),
		node("bu-1", "g1", RoleBU),
	} {
		if err := discovery.Register(ctx, n); err != nil {
			t.Fatalf("Register %s: %v", n.Tag, err)
		}
	}

	// 拉取 g1/ap
	nodes, err := discovery.PullNodes(ctx, "g1", RoleAP, 15*time.Minute)
	if err != nil {
		t.Fatalf("PullNodes g1/ap: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Tag != "ap-1" {
		t.Fatalf("g1/ap: got %v, want [ap-1]", nodes)
	}

	// 拉取 g2/ap
	nodes, err = discovery.PullNodes(ctx, "g2", RoleAP, 15*time.Minute)
	if err != nil {
		t.Fatalf("PullNodes g2/ap: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Tag != "ap-2" {
		t.Fatalf("g2/ap: got %v, want [ap-2]", nodes)
	}

	// 拉取 g1/bu
	nodes, err = discovery.PullNodes(ctx, "g1", RoleBU, 15*time.Minute)
	if err != nil {
		t.Fatalf("PullNodes g1/bu: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Tag != "bu-1" {
		t.Fatalf("g1/bu: got %v, want [bu-1]", nodes)
	}
}

// ── 重复注册（更新节点） ──────────────────────────────────────

func TestRedisDiscovery_RegisterIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	discovery, _, cleanup := newRedisDiscovery(t, "test:idempotent")
	defer cleanup()

	ctx := context.Background()

	// 第一次注册
	if err := discovery.Register(ctx, node("node-1", "g1", RoleAP)); err != nil {
		t.Fatalf("Register (1st): %v", err)
	}

	// 第二次注册（更新）
	if err := discovery.Register(ctx, node("node-1", "g1", RoleAP)); err != nil {
		t.Fatalf("Register (2nd): %v", err)
	}

	// 拉取应只有一条记录
	nodes, err := discovery.PullNodes(ctx, "g1", RoleAP, 15*time.Minute)
	if err != nil {
		t.Fatalf("PullNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
}

// ── 并发 Register ─────────────────────────────────────────────

func TestRedisDiscovery_ConcurrentRegister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	discovery, _, cleanup := newRedisDiscovery(t, "test:concurrent")
	defer cleanup()

	ctx := context.Background()
	const goroutines = 10

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			err := discovery.Register(ctx, node(fmt.Sprintf("node-%d", i), "g1", RoleAP))
			errCh <- err
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent register failed: %v", err)
		}
	}

	// 验证所有节点都已注册
	nodes, err := discovery.PullNodes(ctx, "g1", RoleAP, 15*time.Minute)
	if err != nil {
		t.Fatalf("PullNodes: %v", err)
	}
	if len(nodes) != goroutines {
		t.Fatalf("expected %d nodes, got %d", goroutines, len(nodes))
	}
}

// ── Watch 方法 ─────────────────────────────────────────────────

func TestRedisDiscovery_Watch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	discovery, _, cleanup := newRedisDiscovery(t, "test:watch")
	defer cleanup()

	ctx := context.Background()

	// 注册初始节点
	if err := discovery.Register(ctx, node("node-1", "g1", RoleAP)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// 启动 Watch
	watchCh := discovery.Watch(ctx, "g1", RoleAP, 15*time.Minute)

	// 第一次推送应包含 node-1
	select {
	case nodes := <-watchCh:
		if len(nodes) != 1 {
			t.Fatalf("watch: expected 1 node, got %d", len(nodes))
		}
		if nodes[0].Tag != "node-1" {
			t.Fatalf("watch: expected node-1, got %s", nodes[0].Tag)
		}
	case <-time.After(redisStartTimeout):
		t.Fatal("watch did not receive initial nodes within timeout")
	}

	// Watch 应在 context 取消时关闭
	// (ch 已由 Watch 内部 close，再次读取返回零值 + false)
}
