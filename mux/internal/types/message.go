// Package types 提供 mux 内部共享的基础类型。
package types

import "context"

// Message 是传输层无关的消息格式。
type Message struct {
	Cmd  string
	Head map[string]string
	Body []byte
}

// Source 返回原始 Go context。
func (m *Message) Source() context.Context {
	return context.Background()
}
