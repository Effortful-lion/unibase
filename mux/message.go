package mux

import (
	"github.com/Effortful-lion/unibase/mux/internal/types"
)

// Message 是传输层无关的消息格式。
// Transport 层负责将 HTTP/WS 请求转换成 Message，Pipeline 和 Handler 只处理 Message。
// RPC transport is planned; see Context.Protocol for extensibility.
type Message = types.Message
