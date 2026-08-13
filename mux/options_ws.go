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
