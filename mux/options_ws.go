package mux

import (
	"time"
)

// WithWebSocketPath 设置 WebSocket 升级端点路径，默认为 "/ws"。
func WithWebSocketPath(path string) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.wsPath = path
	}
}

// WithMaxWebSocketConn 设置 WebSocket 最大并发连接数，0 表示不限制。
func WithMaxWebSocketConn(max int) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.maxWSConn = max
	}
}

// WithWebSocketHeartbeat 设置 WebSocket 心跳间隔和 Pong 等待时间。
func WithWebSocketHeartbeat(interval, pongWait time.Duration) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.heartbeatInterval = interval
		o.heartbeatPongWait = pongWait
	}
}

// WithMaxMessageSize 设置 WebSocket 单条消息最大字节数。
func WithMaxMessageSize(size int64) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.maxWSMessageSize = size
	}
}

// WithWebSocketCompression 启用 WebSocket permessage-deflate 压缩。
// level 为压缩级别：1=BestSpeed ~ 9=BestCompression，-1=DefaultCompression。
// 适用于消息体较大的 IM 场景，可显著降低带宽。
// 注意：压缩会增加 CPU 开销，小消息（< 1KB）可能因压缩头开销反而增大。
func WithWebSocketCompression(level int) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.wsCompressionEnabled = true
		o.wsCompressionLevel = level
	}
}

// DisableWS 禁用 WebSocket 传输层（WS Upgrade + WS Cmd 均不启动）。
// 适用于纯 REST 微服务。
func DisableWS() EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.enableWS = false
	}
}
