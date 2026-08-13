package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/redis/go-redis/v9"
)

const redisOpTimeout = 3 * time.Second

// RedisDiscovery 基于 Redis ZSet 实现节点发现。
type RedisDiscovery struct {
	rdb    *redis.Client
	prefix string
	logger *logx.Logger
}

// NewRedisDiscovery 创建 Redis 节点发现。
func NewRedisDiscovery(rdb *redis.Client, prefix string) *RedisDiscovery {
	if prefix == "" {
		prefix = "cluster:nodes"
	}
	return &RedisDiscovery{
		rdb:    rdb,
		prefix: prefix,
		logger: logx.Default().Module("mux"),
	}
}

func (d *RedisDiscovery) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, redisOpTimeout)
}

// zsetKey 返回 ZSet 的 key。
func (d *RedisDiscovery) zsetKey(group string, role Role) string {
	return d.prefix + ":" + role.String() + ":" + group
}

// nodeKey 返回单个节点信息的 key。
func (d *RedisDiscovery) nodeKey(group string, role Role, tag string) string {
	return d.zsetKey(group, role) + ":" + tag
}

// Register 注册/更新当前节点。
func (d *RedisDiscovery) Register(ctx context.Context, node ClusterNode) error {
	node.Ts = time.Now().Unix()

	key := d.zsetKey(node.Group, node.Role)
	nodeKey := d.nodeKey(node.Group, node.Role, node.Tag)

	ctx, cancel := d.withTimeout(ctx)
	defer cancel()

	nodeJSON, err := mustJSON(node)
	if err != nil {
		return fmt.Errorf("marshal node: %w", err)
	}

	pipe := d.rdb.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  float64(node.Ts),
		Member: node.Tag,
	})
	pipe.Set(ctx, nodeKey, nodeJSON, 0)

	_, err = pipe.Exec(ctx)
	if err != nil {
		d.logger.Error("register node failed", logx.Fields{"error": err, "tag": node.Tag})
	}
	return err
}

// Unregister 注销节点。
func (d *RedisDiscovery) Unregister(ctx context.Context, node ClusterNode) error {
	key := d.zsetKey(node.Group, node.Role)
	nodeKey := d.nodeKey(node.Group, node.Role, node.Tag)

	ctx, cancel := d.withTimeout(ctx)
	defer cancel()

	pipe := d.rdb.Pipeline()
	pipe.ZRem(ctx, key, node.Tag)
	pipe.Del(ctx, nodeKey)

	_, err := pipe.Exec(ctx)
	if err != nil {
		d.logger.Error("unregister node failed", logx.Fields{"error": err, "tag": node.Tag})
	}
	return err
}

// PullNodes 拉取指定 group + role 的存活节点。
func (d *RedisDiscovery) PullNodes(ctx context.Context, group string, role Role, ttl time.Duration) ([]ClusterNode, error) {
	key := d.zsetKey(group, role)

	ctx, cancel := d.withTimeout(ctx)
	defer cancel()

	// 清理过期节点
	cutoff := time.Now().Add(-ttl).Unix()
	if _, err := d.rdb.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", cutoff)).Result(); err != nil {
		d.logger.Warn("clean expired nodes failed", logx.Fields{"error": err, "key": key})
	}

	// 获取存活节点
	tags, err := d.rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	nodes := make([]ClusterNode, 0, len(tags))
	for _, tag := range tags {
		nodeKey := d.nodeKey(group, role, tag)
		data, err := d.rdb.Get(ctx, nodeKey).Result()
		if err != nil {
			// 节点数据缺失（可能正在注销），记录警告
			d.logger.Warn("node data missing in redis", logx.Fields{"tag": tag, "error": err})
			continue
		}
		var node ClusterNode
		if err := json.Unmarshal([]byte(data), &node); err != nil {
			d.logger.Error("failed to unmarshal node data", logx.Fields{"tag": tag, "error": err})
			continue
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// Watch 监听节点变化（简化实现，轮询 PullNodes）。
func (d *RedisDiscovery) Watch(ctx context.Context, group string, role Role, ttl time.Duration) <-chan []ClusterNode {
	ch := make(chan []ClusterNode, 1) // 缓冲 1 个结果，避免消费者慢时阻塞
	go func() {
		defer close(ch)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				nodes, err := d.PullNodes(ctx, group, role, ttl)
				if err != nil {
					d.logger.Error("watch pull nodes failed", logx.Fields{"error": err})
					continue
				}
				select {
				case ch <- nodes:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func mustJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
