package mux

import "context"

// Transport 是网络传输层抽象。
type Transport interface {
	// Serve 启动传输层服务，阻塞直到 ctx 取消或发生错误。
	Serve(ctx context.Context) error

	// Close 优雅关闭传输层。
	Close(ctx context.Context) error
}
