package cluster

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/redis/go-redis/v9"
)

// redisIntegrationTest 判断是否运行 Redis 集成测试。
// 设置环境变量 RUN_REDIS_INTEGRATION=1 启用。
func redisIntegrationTest() bool {
	return os.Getenv("RUN_REDIS_INTEGRATION") == "1"
}

// newTestRedis 创建指向本地 Redis 的客户端（用于集成测试）。
func newTestRedis(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
	})
}

// TestRedisDiscovery_Integration 验证 RedisDiscovery 在真实 Redis 上的行为。
// 运行方式：RUN_REDIS_INTEGRATION=1 go test -v -run TestRedisDiscovery_Integration ./internal/cluster/
func TestRedisDiscovery_Integration(t *testing.T) {
	if !redisIntegrationTest() {
		t.Skip("skipping Redis integration test; set RUN_REDIS_INTEGRATION=1 to run")
	}

	ctx := context.Background()
	rdb := newTestRedis("localhost:6379")

	// 验证 Redis 连通性
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis not available at localhost:6379: %v", err)
	}

	logger := logx.Default().Module("cluster.integration")
	discovery := NewRedisDiscovery(rdb, "test:cluster")

	// 清理可能残留的测试数据
	discovery.cleanupTestKeys(ctx, t, "test:cluster", "g1", RoleAP)

	// ── 测试节点 ──
	node1 := ClusterNode{
		Tag:         "node-1",
		ServiceName: "svc",
		Group:       "g1",
		Role:        RoleAP,
		IPPort:      "127.0.0.1:8081",
		ConnectUrl:  "http://127.0.0.1:8081",
	}
	node2 := ClusterNode{
		Tag:         "node-2",
		ServiceName: "svc",
		Group:       "g1",
		Role:        RoleAP,
		IPPort:      "127.0.0.1:8082",
		ConnectUrl:  "http://127.0.0.1:8082",
	}

	// ── 1. 注册节点 ──
	t.Run("Register", func(t *testing.T) {
		if err := discovery.Register(ctx, node1); err != nil {
			t.Fatalf("Register node1 failed: %v", err)
		}
		if err := discovery.Register(ctx, node2); err != nil {
			t.Fatalf("Register node2 failed: %v", err)
		}
		logger.Info("registered 2 nodes")
	})

	// ── 2. 拉取节点 ──
	t.Run("PullNodes", func(t *testing.T) {
		nodes, err := discovery.PullNodes(ctx, "g1", RoleAP, 10*time.Minute)
		if err != nil {
			t.Fatalf("PullNodes failed: %v", err)
		}
		if len(nodes) != 2 {
			t.Fatalf("PullNodes returned %d nodes, want 2", len(nodes))
		}
		// 验证节点数据
		found1 := false
		found2 := false
		for _, n := range nodes {
			if n.Tag == "node-1" {
				found1 = true
				if n.IPPort != "127.0.0.1:8081" {
					t.Errorf("node1 IPPort = %s, want 127.0.0.1:8081", n.IPPort)
				}
			}
			if n.Tag == "node-2" {
				found2 = true
			}
		}
		if !found1 || !found2 {
			t.Errorf("PullNodes missing expected nodes: node1=%v, node2=%v", found1, found2)
		}
		logger.Info("pulled nodes", logx.Fields{"count": len(nodes)})
	})

	// ── 3. TTL 过期过滤 ──
	t.Run("PullNodes_ExpiredFilter", func(t *testing.T) {
		// 手动将 node1 的 TTL 设为过期
		key := discovery.zsetKey("g1", RoleAP)
		cutoff := time.Now().Add(-20 * time.Minute).Unix()
		if _, err := rdb.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", cutoff)).Result(); err != nil {
			t.Fatalf("ZRemRangeByScore failed: %v", err)
		}

		nodes, err := discovery.PullNodes(ctx, "g1", RoleAP, 5*time.Minute)
		if err != nil {
			t.Fatalf("PullNodes failed: %v", err)
		}
		// node1 已过期，node2 仍存活
		if len(nodes) != 1 {
			t.Fatalf("PullNodes after expiry returned %d nodes, want 1", len(nodes))
		}
		if nodes[0].Tag != "node-2" {
			t.Errorf("expected node-2, got %s", nodes[0].Tag)
		}
		logger.Info("expired node filtered correctly")
	})

	// ── 4. 注销节点 ──
	t.Run("Unregister", func(t *testing.T) {
		if err := discovery.Unregister(ctx, node2); err != nil {
			t.Fatalf("Unregister node2 failed: %v", err)
		}

		nodes, err := discovery.PullNodes(ctx, "g1", RoleAP, 10*time.Minute)
		if err != nil {
			t.Fatalf("PullNodes after unregister failed: %v", err)
		}
		if len(nodes) != 0 {
			t.Fatalf("PullNodes after unregister returned %d nodes, want 0", len(nodes))
		}
		logger.Info("unregistered node2 successfully")
	})

	// ── 5. 重新注册并验证 Watch ──
	t.Run("Watch", func(t *testing.T) {
		// 重新注册 node1
		if err := discovery.Register(ctx, node1); err != nil {
			t.Fatalf("Register node1 failed: %v", err)
		}

		watchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()

		ch := discovery.Watch(watchCtx, "g1", RoleAP, 10*time.Minute)

		select {
		case nodes, ok := <-ch:
			if !ok {
				t.Fatal("Watch channel closed unexpectedly")
			}
			if len(nodes) != 1 {
				t.Errorf("Watch returned %d nodes, want 1", len(nodes))
			}
			if len(nodes) > 0 && nodes[0].Tag != "node-1" {
				t.Errorf("Watch returned node %s, want node-1", nodes[0].Tag)
			}
			logger.Info("watch received nodes", logx.Fields{"count": len(nodes)})
		case <-time.After(5 * time.Second):
			t.Fatal("Watch timeout waiting for nodes")
		}
	})

	// 清理
	discovery.cleanupTestKeys(ctx, t, "test:cluster", "g1", RoleAP)
	logger.Info("integration test completed")
}

// cleanupTestKeys 清理测试数据（辅助函数）。
func (d *RedisDiscovery) cleanupTestKeys(ctx context.Context, t *testing.T, prefix string, group string, role Role) {
	t.Helper()
	// 构造 key 模式，直接使用 Redis 的 key 格式
	pattern := prefix + ":" + role.String() + ":" + group + ":*"
	keys, err := d.rdb.Keys(ctx, pattern).Result()
	if err != nil {
		t.Logf("cleanup: Keys %s failed: %v", pattern, err)
		return
	}
	if len(keys) > 0 {
		if err := d.rdb.Del(ctx, keys...).Err(); err != nil {
			t.Logf("cleanup: Del keys failed: %v", err)
		}
	}

	// 清理 ZSet key
	zsetKey := prefix + ":" + role.String() + ":" + group
	if err := d.rdb.Del(ctx, zsetKey).Err(); err != nil {
		t.Logf("cleanup: Del zset %s failed: %v", zsetKey, err)
	}
}
