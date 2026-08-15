package cluster

import (
	"sort"
	"sync"

	"github.com/Effortful-lion/unibase/tools/hash"
)

// HashRing 实现一致性哈希算法，用于将 user_id / session_id 映射到集群节点。
//
// 核心特性：
//   - 虚拟节点（virtual nodes）：每个物理节点对应多个虚拟节点，均匀分布避免倾斜
//   - CRC32 哈希：快速、确定性，适合节点映射场景
//   - 线程安全：读写操作均通过 RWMutex 保护
//   - 增量更新：Add/Remove 操作只更新受影响的部分，不重建整个环
type HashRing struct {
	mu       sync.RWMutex
	virtuals int // 每个物理节点的虚拟节点数
	hasher   func([]byte) uint32
	ring     []uint32               // 排序的哈希环位置
	nodes    map[uint32]ClusterNode // 虚拟节点哈希 → 物理节点
	nodeKeys map[string]uint32      // 物理节点 Tag → 一个代表哈希（用于 Remove）
}

// HashRingOption 配置 HashRing 的行为。
type HashRingOption func(*HashRing)

// WithVirtualNodes 设置每个物理节点的虚拟节点数量。
// 默认 150，值越大分布越均匀，但构建和查询开销略增。
func WithVirtualNodes(n int) HashRingOption {
	return func(hr *HashRing) {
		hr.virtuals = n
	}
}

// WithHasher 设置自定义哈希函数。
// 默认使用 CRC32。
func WithHasher(fn func([]byte) uint32) HashRingOption {
	return func(hr *HashRing) {
		hr.hasher = fn
	}
}

// DefaultHashRingOptions 返回默认配置。
func DefaultHashRingOptions() HashRingOption {
	return func(hr *HashRing) {
		hr.virtuals = 150
		hr.hasher = func(data []byte) uint32 {
			return hash.CRC32Uint32(data)
		}
	}
}

// NewHashRing 创建空 HashRing，需调用 Add 填充节点。
func NewHashRing(opts ...HashRingOption) *HashRing {
	hr := &HashRing{
		virtuals: 150,
		nodes:    make(map[uint32]ClusterNode),
		nodeKeys: make(map[string]uint32),
	}
	DefaultHashRingOptions()(hr)
	for _, opt := range opts {
		opt(hr)
	}
	return hr
}

// Add 将节点添加到哈希环。
// 如果节点已存在，先 Remove 再添加（幂等）。
func (hr *HashRing) Add(node ClusterNode) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	// 已存在则先移除
	if _, ok := hr.nodeKeys[node.Tag]; ok {
		hr.removeLocked(node.Tag)
	}

	key := hr.hash(node.Tag)
	for i := 0; i < hr.virtuals; i++ {
		virtualKey := hr.hashWithIndex(key, i)
		hr.ring = append(hr.ring, virtualKey)
		hr.nodes[virtualKey] = node
	}
	hr.nodeKeys[node.Tag] = key
	sort.Slice(hr.ring, func(i, j int) bool {
		return hr.ring[i] < hr.ring[j]
	})
}

// Remove 从哈希环移除节点。
func (hr *HashRing) Remove(node ClusterNode) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.removeLocked(node.Tag)
}

func (hr *HashRing) removeLocked(tag string) {
	key, ok := hr.nodeKeys[tag]
	if !ok {
		return
	}
	delete(hr.nodeKeys, tag)

	for i := 0; i < hr.virtuals; i++ {
		virtualKey := hr.hashWithIndex(key, i)
		delete(hr.nodes, virtualKey)
	}
	hr.rebuildRingLocked()
}

// rebuildRingLocked 从 nodes map 重建 ring 切片（仅在持有锁时调用）。
func (hr *HashRing) rebuildRingLocked() {
	hr.ring = make([]uint32, 0, len(hr.nodes))
	for k := range hr.nodes {
		hr.ring = append(hr.ring, k)
	}
	sort.Slice(hr.ring, func(i, j int) bool {
		return hr.ring[i] < hr.ring[j]
	})
}

// Get 根据 key 查找对应的物理节点。
// 如果环为空，返回零值 ClusterNode。
// 顺时针查找第一个哈希值 >= key.hash 的虚拟节点，找不到则返回环首节点。
func (hr *HashRing) Get(key string) (ClusterNode, bool) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	if len(hr.ring) == 0 {
		return ClusterNode{}, false
	}

	h := hr.hash(string(key))
	idx := sort.Search(len(hr.ring), func(i int) bool {
		return hr.ring[i] >= h
	})
	if idx == len(hr.ring) {
		idx = 0 // 环形：到达末尾后回到开头
	}
	node, ok := hr.nodes[hr.ring[idx]]
	return node, ok
}

// Size 返回物理节点数量。
func (hr *HashRing) Size() int {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	return len(hr.nodeKeys)
}

// Set 批量替换哈希环上的所有节点（原子操作）。
func (hr *HashRing) Set(nodes []ClusterNode) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.nodes = make(map[uint32]ClusterNode, len(nodes)*hr.virtuals)
	hr.nodeKeys = make(map[string]uint32, len(nodes))
	hr.ring = make([]uint32, 0, len(nodes)*hr.virtuals)

	for _, node := range nodes {
		key := hr.hash(node.Tag)
		hr.nodeKeys[node.Tag] = key
		for i := 0; i < hr.virtuals; i++ {
			virtualKey := hr.hashWithIndex(key, i)
			hr.ring = append(hr.ring, virtualKey)
			hr.nodes[virtualKey] = node
		}
	}
	sort.Slice(hr.ring, func(i, j int) bool {
		return hr.ring[i] < hr.ring[j]
	})
}

// hash 计算字符串的哈希值。
func (hr *HashRing) hash(s string) uint32 {
	if hr.hasher != nil {
		return hr.hasher([]byte(s))
	}
	return hash.CRC32Uint32([]byte(s))
}

// hashWithIndex 将节点 key 和虚拟节点索引组合哈希，确保分布均匀。
func (hr *HashRing) hashWithIndex(key uint32, index int) uint32 {
	data := []byte{byte(key >> 24), byte(key >> 16), byte(key >> 8), byte(key)}
	data = append(data, byte(index), byte(index>>8))
	return hr.hasher(data)
}

// CRC32Uint32 返回 []byte 的 CRC32 (IEEE) 哈希值（uint32）。
func CRC32Uint32(data []byte) uint32 {
	return hash.CRC32Uint32(data)
}
