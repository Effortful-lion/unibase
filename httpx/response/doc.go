// Package response 定义框架无关的响应写入接口，并提供 Gin 实现。
//
// 封装 Gin 没有或薄的能力：Stream 流式响应、SSE 服务器推送、HTML / File 响应。
//
// 快速开始：
//
//	w := httpx.Gin(c)
//	w.SSE(func(w httpx.SSEWriter) error { ... })
//	w.Stream(200, "text/plain", reader)
//
// 能力：ResponseWriter 接口（Stream / SSE / HTML / File / Header），
// GinResponseWriter 实现，SSEWriter 适配器。
package response
