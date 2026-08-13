package mux

// Session 是统一会话接口。
//
// HTTP 模式下生命周期为 per-request（请求结束即销毁）。
// WebSocket 模式下生命周期为 per-connection（连接保活、房间管理）。
type Session interface {
	// ID 返回会话唯一标识。
	// HTTP: 请求唯一 ID；WebSocket: 连接唯一 ID。
	ID() string

	// UserID 返回当前认证用户 ID，未认证时返回空字符串。
	UserID() string

	// SetState 在会话中存储键值对。
	// HTTP: 仅在当前请求有效；WebSocket: 整个连接有效。
	SetState(key string, value any)

	// GetState 从会话中读取键值对，第二个返回值表示键是否存在。
	GetState(key string) (any, bool)
}
