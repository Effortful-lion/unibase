package mux

// WithHTTPAddr 设置 HTTP 监听地址，默认 ":8080"。
func WithHTTPAddr(addr string) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.httpAddr = addr
	}
}
