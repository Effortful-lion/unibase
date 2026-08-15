package mux

// WithHTTPAddr 设置 HTTP 监听地址，默认 ":8080"。
func WithHTTPAddr(addr string) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.httpAddr = addr
	}
}

// DisableHTTP 禁用 HTTP 传输层（REST 路由 + HTTP Cmd 均不启动）。
// 适用于纯 WebSocket 服务。
func DisableHTTP() EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.enableHTTP = false
	}
}
