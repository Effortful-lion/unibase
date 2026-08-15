package cluster

import (
	"context"
	"sync"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux/internal/types"
)

// ClusterManager 管理集群节点生命周期：注册、心跳、节点发现。
type ClusterManager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	self      ClusterNode
	discovery Discovery
	nodes     *sync.Map // tag → ClusterNode
	forward   *Forwarder
	ticker    *time.Ticker
	heartbeat time.Duration
	nodeTTL   time.Duration
	logger    *logx.Logger
	wg        sync.WaitGroup
	nodesMu   sync.RWMutex
	closeFn   func() error // 清理资源（如关闭 Redis 连接）

	// 一致性哈希环（user_id / session_id → 节点路由）
	ring *HashRing
}

// NewClusterManager 创建集群管理器。
func NewClusterManager(ctx context.Context, self ClusterNode, discovery Discovery, opts ...ClusterOption) *ClusterManager {
	cctx, cancel := context.WithCancel(ctx)
	o := defaultClusterOptions()
	for _, opt := range opts {
		opt(o)
	}

	logger := logx.Default().Module("mux")

	cm := &ClusterManager{
		ctx:       cctx,
		cancel:    cancel,
		self:      self,
		discovery: discovery,
		nodes:     &sync.Map{},
		forward:   NewForwarder(logger, o.cmdPath),
		heartbeat: o.heartbeatInterval,
		nodeTTL:   o.nodeTTL,
		logger:    logger,
		closeFn:   o.closeFn,
		ring:      NewHashRing(DefaultHashRingOptions()),
	}

	return cm
}

// WithCloseFn 设置集群管理器停止时的清理函数（如关闭 Redis 连接）。
func WithCloseFn(fn func() error) ClusterOption {
	return func(o *clusterOptions) {
		o.closeFn = fn
	}
}

// Start 启动集群管理器（开始心跳和节点发现）。
func (cm *ClusterManager) Start() {
	cm.ticker = time.NewTicker(cm.heartbeat)
	cm.wg.Add(2)
	go func() {
		defer cm.wg.Done()
		cm.heartbeatLoop()
	}()
	go func() {
		defer cm.wg.Done()
		cm.discoveryLoop()
	}()
}

// Stop 停止集群管理器。
func (cm *ClusterManager) Stop() {
	if cm.ticker != nil {
		cm.ticker.Stop()
	}
	cm.cancel()
	cm.wg.Wait()

	// 清理外部资源（如关闭 Redis 连接）
	if cm.closeFn != nil {
		_ = cm.closeFn()
	}
}

// GetNodes 获取同组同角色的存活节点（排除自身）。
func (cm *ClusterManager) GetNodes(group string, role Role) []ClusterNode {
	cm.nodesMu.RLock()
	defer cm.nodesMu.RUnlock()

	nodes := make([]ClusterNode, 0)
	cm.nodes.Range(func(_, v any) bool {
		node := v.(ClusterNode)
		if node.Tag == cm.self.Tag {
			return true
		}
		if node.Group == group && node.Role == role && node.IsAlive(cm.nodeTTL) {
			nodes = append(nodes, node)
		}
		return true
	})
	return nodes
}

// Forward 将消息转发到目标节点。
func (cm *ClusterManager) Forward(ctx context.Context, target *ClusterNode, msg *types.Message) error {
	return cm.forward.Forward(ctx, target, msg)
}

// RouteUser 根据 user_id 通过一致性哈希查找目标节点。
// 返回 (nil, false) 表示无可用节点。
func (cm *ClusterManager) RouteUser(userID string) (ClusterNode, bool) {
	return cm.ring.Get(userID)
}

// heartbeatLoop 定时向 Redis 注册当前节点。
func (cm *ClusterManager) heartbeatLoop() {
	for {
		if cm.ticker == nil {
			return
		}
		select {
		case <-cm.ticker.C:
			if err := cm.discovery.Register(cm.ctx, cm.self); err != nil {
				cm.logger.Error("heartbeat register failed", logx.Fields{"error": err, "tag": cm.self.Tag})
			}
		case <-cm.ctx.Done():
			if err := cm.discovery.Unregister(cm.ctx, cm.self); err != nil {
				cm.logger.Warn("heartbeat unregister failed", logx.Fields{"error": err, "tag": cm.self.Tag})
			}
			return
		}
	}
}

// discoveryLoop 定时拉取节点列表，增量更新而非全量替换。
// 增量更新只删除不再存在的旧节点、添加新发现的节点，避免全量替换期间阻塞读者。
// 同时同步一致性哈希环，保证 RouteUser 路由的准确性。
func (cm *ClusterManager) discoveryLoop() {
	ticker := time.NewTicker(cm.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			nodes, err := cm.discovery.PullNodes(cm.ctx, cm.self.Group, cm.self.Role, cm.nodeTTL)
			if err != nil {
				cm.logger.Error("pull nodes failed", logx.Fields{"error": err})
				continue
			}

			// 构建新节点集合（排除自身）
			newNodes := make(map[string]ClusterNode, len(nodes))
			var nodeList []ClusterNode
			for _, node := range nodes {
				if node.Tag != cm.self.Tag {
					newNodes[node.Tag] = node
					nodeList = append(nodeList, node)
				}
			}

			// 增量更新：只添加和删除变化的节点，避免全量替换阻塞读者
			cm.nodesMu.Lock()
			var toDelete []string
			cm.nodes.Range(func(key, _ any) bool {
				tag := key.(string)
				if _, ok := newNodes[tag]; !ok {
					toDelete = append(toDelete, tag)
				}
				return true
			})
			for _, tag := range toDelete {
				cm.nodes.Delete(tag)
			}
			for tag, node := range newNodes {
				cm.nodes.Store(tag, node)
			}
			cm.nodesMu.Unlock()

			// 同步一致性哈希环
			cm.ring.Set(nodeList)
		case <-cm.ctx.Done():
			return
		}
	}
}

// ClusterOption 配置集群管理器的可选参数。
type ClusterOption func(*clusterOptions)

type clusterOptions struct {
	heartbeatInterval time.Duration
	nodeTTL           time.Duration
	cmdPath           string
	closeFn           func() error
}

func defaultClusterOptions() *clusterOptions {
	return &clusterOptions{
		heartbeatInterval: 5 * time.Second,
		nodeTTL:           15 * time.Second,
		cmdPath:           "/v1/cmd",
	}
}

// WithClusterHeartbeatInterval 设置心跳间隔。
func WithClusterHeartbeatInterval(d time.Duration) ClusterOption {
	return func(o *clusterOptions) {
		o.heartbeatInterval = d
	}
}

// WithClusterNodeTTL 设置节点 TTL。
func WithClusterNodeTTL(d time.Duration) ClusterOption {
	return func(o *clusterOptions) {
		o.nodeTTL = d
	}
}

// WithClusterCmdPath 设置集群转发的 Cmd 入口路径。
func WithClusterCmdPath(path string) ClusterOption {
	return func(o *clusterOptions) {
		o.cmdPath = path
	}
}

// SetCloseFn 设置停止时的清理函数（如关闭 Redis 连接）。
func (cm *ClusterManager) SetCloseFn(fn func() error) {
	cm.closeFn = fn
}
