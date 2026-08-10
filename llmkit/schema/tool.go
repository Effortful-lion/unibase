// Package schema 提供了 llmkit 的扩展类型定义，包括工具定义和工具调用。
package schema

// ToolChoice 控制模型是否/如何调用工具。
type ToolChoice string

const (
	// ToolChoiceNone 表示模型不应调用任何工具。
	ToolChoiceNone ToolChoice = "none"
	// ToolChoiceAuto 表示模型自行决定是否调用工具（默认）。
	ToolChoiceAuto ToolChoice = "auto"
	// ToolChoiceRequired 表示模型必须调用至少一个工具。
	ToolChoiceRequired ToolChoice = "required"
)

// ToolInfo 是工具的元数据描述，用于在 LLM 请求中声明可用工具。
type ToolInfo struct {
	// Name 是工具的唯一名称，LLM 在 tool_call 中使用此名称。
	Name string `json:"name"`
	// Description 是工具的功能描述，帮助 LLM 理解何时使用此工具。
	Description string `json:"description"`
	// Parameters 是 JSON Schema 格式的参数定义，描述工具接受的参数。
	Parameters map[string]any `json:"parameters"`
	// Strict 为 true 时，LLM 必须严格按照参数定义调用工具。
	Strict bool `json:"strict,omitempty"`
}

// ToolCall 是模型返回的工具调用请求。
//
// Arguments 在流式传输过程中会逐步填充（增量模式）。
type ToolCall struct {
	// ID 是本次工具调用的唯一标识，用于关联 ToolResult。
	ID string `json:"id"`
	// Name 是要调用的工具名称。
	Name string `json:"name"`
	// Arguments 是 JSON 格式的调用参数。
	// 流式场景下，此字段会逐步累积完整 JSON。
	Arguments string `json:"arguments"`
}

// ToolResult 是工具执行的结果。
type ToolResult struct {
	// CallID 关联到对应的 ToolCall.ID。
	CallID string `json:"call_id"`
	// Result 是工具执行成功后的返回内容。
	Result string `json:"result"`
	// Error 是工具执行失败时的错误信息，成功时为 nil。
	Error error `json:"-"`
}
