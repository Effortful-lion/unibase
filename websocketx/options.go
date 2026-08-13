package websocketx

import (
	"time"
)

// ── Hub 配置选项 ──────────────────────────────────────────────

// HubOption 配置 Hub 的可选参数。
type HubOption func(*Hub)

// WithHeartbeat 开启心跳保活。
// interval: 发送 Ping 的间隔。
// pongWait: 等待 Pong 的超时时长，超时后关闭连接。
func WithHeartbeat(interval, pongWait time.Duration) HubOption {
	return func(h *Hub) {
		h.heartbeatInterval = interval
		h.heartbeatPongWait = pongWait
	}
}

// WithCloseReason 设置关闭原因（用于 Kick 和 Shutdown 时自定义 Close 帧的 reason）。
// 默认使用 CloseReasonKicked 和 CloseReasonServerShutdown。
func WithCloseReason(kickReason, shutdownReason CloseReason) HubOption {
	return func(h *Hub) {
		h.closeReasonKick = kickReason
		h.closeReasonShutdown = shutdownReason
	}
}

// WithMaxMessageSize 设置全局最大消息大小（字节），0 表示不限制。
func WithMaxMessageSize(n int64) HubOption {
	return func(h *Hub) {
		h.maxMessageSize = n
	}
}

// WithMaxMessageRate 设置单 Session 每秒最大消息数（QPS），0 表示不限制。
// 超出限流时自动断开连接并发送 CloseReasonInvalidMessage。
func WithMaxMessageRate(qps int) HubOption {
	return func(h *Hub) {
		h.messageRate = qps
	}
}

// WithMetrics 设置监控埋点回调。
// onConnect / onDisconnect / onMessage / onBroadcast 为 nil 时跳过对应埋点。
func WithMetrics(onConnect, onDisconnect, onMessage, onBroadcast MetricsHook) HubOption {
	return func(h *Hub) {
		h.metrics.onConnect = onConnect
		h.metrics.onDisconnect = onDisconnect
		h.metrics.onMessage = onMessage
		h.metrics.onBroadcast = onBroadcast
	}
}

// WithOnConnect 设置连接建立时的回调（异步执行）。
func WithOnConnect(fn func(*Session)) HubOption {
	return func(h *Hub) {
		h.onConnect = fn
	}
}

// WithOnDisconnect 设置连接断开时的回调。
func WithOnDisconnect(fn func(*Session)) HubOption {
	return func(h *Hub) {
		h.onDisconnect = fn
	}
}
