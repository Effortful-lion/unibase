// Package rediskeyevent 提供 Redis 键空间事件（Keyspace Notification）订阅能力。
//
// Redis 的键过期事件（expired）可用于实现状态自动同步：
// 例如，验证码过期后自动失效、订单超时后自动取消。
//
// 前置条件：Redis 需启用键空间通知：
//
//	CONFIG SET notify-keyspace-events Ex
//
// 本包会在 Subscribe 时自动尝试启用，但仍建议在 Redis 配置中持久化该设置。
//
// 快速开始：
//
//	// 订阅所有匹配 "code:*" 的键过期事件
//	sub, err := rediskeyevent.Subscribe(rdb, 0, "code:*", func(key string) {
//	    fmt.Println("验证码过期:", key)
//	})
//	defer sub.Close()
//
// 能力：Subscribe（订阅匹配模式的键过期事件）。
package rediskeyevent
