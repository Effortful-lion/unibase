// Package cluster 提供三层集群架构（MIX → AP+BU）。
//
// 节点发现：基于 Redis ZSet 的注册与拉取。
// 消息转发：通过 HTTP 调用目标节点的 Cmd 入口。
// 节点路由：基于一致性哈希（HashRing）的 user_id → 节点映射。
package cluster
