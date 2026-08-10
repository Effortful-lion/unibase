// Package schema 提供了 llmkit 的扩展类型定义。
package schema

import "encoding/json"

// ToolChoice 控制模型是否/如何调用工具。
type ToolChoice string

const (
	ToolChoiceNone     ToolChoice = "none"
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceRequired ToolChoice = "required"
)

// ToolInfo 是工具的元数据描述，用于传递给模型。
type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Strict      bool
}

// ToolCall 是模型返回的工具调用。
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// ToolResult 是工具执行结果。
type ToolResult struct {
	CallID string
	Result string
	Error  error
}
