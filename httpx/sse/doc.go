// Package sse 提供 Server-Sent Events 事件构造和写入能力。
//
// 快速开始：
//
//	w := sse.NewWriter(c.Writer)
//	w.WriteEvent(sse.Event{Data: []byte("hello")})
//	w.Flush()
//
// 能力：Event 结构（ID / Data / Event / Retry / Comment）、
// MarshalTo 序列化、Writer 便捷写入器。
// 仅依赖 io.Writer，不绑定任何 Web 框架。
package sse
