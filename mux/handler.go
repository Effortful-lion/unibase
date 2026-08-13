package mux

// Handler 是消息处理函数签名。
type Handler func(ctx *Context) error

// Middleware 是 Pipeline 中间件签名。
// 通过闭包实现链式调用，不需要额外的接口或注册表。
type Middleware func(next Handler) Handler
