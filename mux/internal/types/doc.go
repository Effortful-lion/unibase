// Package types 提供 mux 内部跨子包共享的基础类型。
//
// 当前包含：
//   - Message：传输层无关的消息格式，由 Transport 层将 HTTP/WS/RPC 请求转换而成
//
// Pipeline 和 Handler 只处理 Message，不感知底层传输协议。
package types
