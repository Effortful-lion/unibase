package cluster

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeNode 创建测试用 ClusterNode。
func fakeNode(tag, group string, role Role) ClusterNode {
	return ClusterNode{
		Tag:         tag,
		ServiceName: "test",
		Group:       group,
		Role:        role,
		IPPort:      "127.0.0.1:8080",
		ConnectUrl:  "http://127.0.0.1:8080",
	}
}

// ── HashRing 基础 ────────────────────────────────────────────

func TestHashRing_EmptyRing(t *testing.T) {
	hr := NewHashRing()
	_, ok := hr.Get("user_1")
	require.False(t, ok, "empty ring should return no node")
	require.Equal(t, 0, hr.Size())
}

func TestHashRing_AddAndGet(t *testing.T) {
	hr := NewHashRing()
	n1 := fakeNode("node-1", "g1", RoleMix)
	n2 := fakeNode("node-2", "g1", RoleMix)
	n3 := fakeNode("node-3", "g1", RoleMix)

	hr.Add(n1)
	hr.Add(n2)
	hr.Add(n3)

	require.Equal(t, 3, hr.Size())

	// 同一个 key 应该总是路由到同一个节点
	node, ok := hr.Get("user_123")
	require.True(t, ok)
	node2, ok := hr.Get("user_123")
	require.True(t, ok)
	require.Equal(t, node.Tag, node2.Tag)
}

func TestHashRing_Distribution(t *testing.T) {
	hr := NewHashRing(WithVirtualNodes(50))
	nodes := []ClusterNode{
		fakeNode("ap-1", "im", RoleAP),
		fakeNode("ap-2", "im", RoleAP),
		fakeNode("ap-3", "im", RoleAP),
	}
	for _, n := range nodes {
		hr.Add(n)
	}

	// 1000 个 user_id 应该均匀分布到 3 个节点
	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		key := "user_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		node, ok := hr.Get(key)
		require.True(t, ok)
		counts[node.Tag]++
	}

	// 每个节点至少应该有 15% 的流量（理想 33.3%）
	for tag, count := range counts {
		require.Greater(t, float64(count)/1000, 0.15,
			"node %s got only %.1f%% of traffic, expected ~33%%", tag, float64(count)/1000*100)
	}
}

func TestHashRing_Remove(t *testing.T) {
	hr := NewHashRing()
	n1 := fakeNode("node-1", "g1", RoleMix)
	n2 := fakeNode("node-2", "g1", RoleMix)
	n3 := fakeNode("node-3", "g1", RoleMix)

	hr.Add(n1)
	hr.Add(n2)
	hr.Add(n3)

	hr.Remove(n2)
	require.Equal(t, 2, hr.Size())

	// 原来路由到 node-2 的 key 现在应该路由到 node-1 或 node-3
	node, ok := hr.Get("user_999")
	require.True(t, ok)
	require.NotEqual(t, "node-2", node.Tag)
}

// ── HashRing 幂等性 ──────────────────────────────────────────

func TestHashRing_AddIdempotent(t *testing.T) {
	hr := NewHashRing()
	n1 := fakeNode("node-1", "g1", RoleMix)
	hr.Add(n1)
	hr.Add(n1) // 重复添加
	hr.Add(n1) // 再重复
	require.Equal(t, 1, hr.Size())

	node, ok := hr.Get("user_1")
	require.True(t, ok)
	require.Equal(t, "node-1", node.Tag)
}

func TestHashRing_RemoveNonexistent(t *testing.T) {
	hr := NewHashRing()
	n1 := fakeNode("node-1", "g1", RoleMix)
	hr.Add(n1)
	hr.Remove(fakeNode("nonexistent", "g1", RoleMix)) // 不应 panic
	require.Equal(t, 1, hr.Size())
}

// ── HashRing Set 批量替换 ────────────────────────────────────

func TestHashRing_Set(t *testing.T) {
	hr := NewHashRing()
	hr.Add(fakeNode("old-1", "g1", RoleMix))
	hr.Add(fakeNode("old-2", "g1", RoleMix))
	require.Equal(t, 2, hr.Size())

	hr.Set([]ClusterNode{
		fakeNode("new-1", "g1", RoleMix),
		fakeNode("new-2", "g1", RoleMix),
		fakeNode("new-3", "g1", RoleMix),
	})
	require.Equal(t, 3, hr.Size())

	_, ok := hr.Get("any-key")
	require.True(t, ok)
}

// ── 环的连续性 ──────────────────────────────────────────────

func TestHashRing_RingWrapsAround(t *testing.T) {
	hr := NewHashRing(WithVirtualNodes(10))
	hr.Add(fakeNode("only-node", "g1", RoleMix))

	node, ok := hr.Get("zzzzzzzzzzzzzz")
	require.True(t, ok)
	require.Equal(t, "only-node", node.Tag, "single node should handle all keys")
}

// ── ClusterManager.RouteUser 集成 ────────────────────────────

func TestClusterManager_RouteUser(t *testing.T) {
	cm := &ClusterManager{
		self: fakeNode("self", "im", RoleAP),
		ring: NewHashRing(WithVirtualNodes(50)),
	}

	cm.ring.Add(fakeNode("ap-1", "im", RoleAP))
	cm.ring.Add(fakeNode("ap-2", "im", RoleAP))
	cm.ring.Add(fakeNode("ap-3", "im", RoleAP))

	// 同一个 user_id 总是路由到同一个节点
	node1, ok := cm.RouteUser("user_alice")
	require.True(t, ok)
	node2, ok := cm.RouteUser("user_alice")
	require.True(t, ok)
	require.Equal(t, node1.Tag, node2.Tag)

	// 不同 user_id 可能路由到不同节点（验证分布）
	nodes := map[string]bool{}
	for i := 0; i < 100; i++ {
		key := "user_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		n, ok := cm.RouteUser(key)
		if ok {
			nodes[n.Tag] = true
		}
	}
	require.Greater(t, len(nodes), 1, "expected distribution across multiple nodes")
}

func TestClusterManager_RouteUser_EmptyCluster(t *testing.T) {
	cm := &ClusterManager{
		self: fakeNode("self", "im", RoleAP),
		ring: NewHashRing(),
	}

	_, ok := cm.RouteUser("user_1")
	require.False(t, ok)
}
