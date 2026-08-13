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

// WithHandlerTimeout 设置单条消息处理超时。
// 0 表示不限制（默认）。建议生产环境设置合理上限（如 5s）。
func WithHandlerTimeout(d time.Duration) HubOption {
	return func(h *Hub) {
		h.handlerTimeout = d
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

// WithSessionInit 设置连接建立后的 Session 初始化回调。
// 回调在每个新连接创建 Session 后、消息处理开始前执行一次。
// 典型用途：从 HTTP 请求上下文注入 JWT token 到 Session meta。
func WithSessionInit(fn func(*Session)) HubOption {
	return func(h *Hub) {
		h.initSessionFn = fn
	}
}
