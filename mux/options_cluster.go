package mux

import (
	"time"

	"github.com/Effortful-lion/unibase/mux/internal/cluster"
)

// Role 标识节点在集群中的角色。
type Role = cluster.Role

const (
	RoleMix Role = iota
	RoleAP
	RoleBU
)

// WithClusterEnabled 启用或禁用集群功能。
func WithClusterEnabled(enabled bool) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.clusterEnabled = enabled
	}
}

// WithClusterRole 设置当前节点在集群中的角色。
func WithClusterRole(role Role) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.clusterRole = role
	}
}

// WithClusterRedis 设置 Redis 地址（用于节点发现）。
func WithClusterRedis(addr string) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.clusterRedisAddr = addr
	}
}

// WithClusterHeartbeatInterval 设置集群心跳间隔。
func WithClusterHeartbeatInterval(d time.Duration) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.clusterHeartbeatInterval = d
	}
}

// WithClusterNodeTTL 设置节点 TTL（超时未心跳则判定为离线）。
func WithClusterNodeTTL(d time.Duration) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.clusterNodeTTL = d
	}
}

// WithClusterServiceName 设置服务名称。
func WithClusterServiceName(name string) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.clusterServiceName = name
	}
}

// WithClusterGroup 设置集群分组。
func WithClusterGroup(group string) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.clusterGroup = group
	}
}

// WithClusterAdvertiseAddr 设置集群节点对外通告的地址（用于节点间通信）。
// 默认等于监听地址，但监听地址带端口前缀（如 ":8080"）时无效，必须显式设置。
func WithClusterAdvertiseAddr(addr string) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.clusterAdvertiseAddr = addr
	}
}
