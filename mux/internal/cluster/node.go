// Package cluster 提供三层集群架构（MIX → AP+BU）。
package cluster

import (
	"context"
	"time"
)

// Role 标识节点在集群中的角色。
type Role uint8

const (
	// RoleMix 单机混合模式（AP + BU 合一）。
	RoleMix Role = iota
	// RoleAP 接入层（负责 HTTP/WS 接入 + 认证 + 限流）。
	RoleAP
	// RoleBU 业务层（只执行业务逻辑，不接受客户端直连）。
	RoleBU
)

var roleStrings = map[Role]string{
	RoleMix: "mix",
	RoleAP:  "ap",
	RoleBU:  "bu",
}

// String 返回角色的人类可读字符串。
func (r Role) String() string {
	if s, ok := roleStrings[r]; ok {
		return s
	}
	return "unknown"
}

// ClusterNode 描述集群中的一个节点。
type ClusterNode struct {
	Tag         string
	ServiceName string
	Group       string
	Role        Role
	Env         string
	IPPort      string
	ConnectUrl  string
	Ts          int64
}

// IsAlive 检查节点是否在 TTL 内有心跳。
func (n *ClusterNode) IsAlive(ttl time.Duration) bool {
	return time.Since(time.Unix(n.Ts, 0)) < ttl
}

// Discovery 定义节点发现接口。
type Discovery interface {
	Register(ctx context.Context, node ClusterNode) error
	Unregister(ctx context.Context, node ClusterNode) error
	PullNodes(ctx context.Context, group string, role Role, ttl time.Duration) ([]ClusterNode, error)
	Watch(ctx context.Context, group string, role Role, ttl time.Duration) <-chan []ClusterNode
}
