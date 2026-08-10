package types

// EventType 是 LLM 流式响应中的事件类型。
type EventType int

const (
	// EventStart 表示整个流式响应开始。
	EventStart EventType = iota

	// EventTextStart 表示文本内容块开始。
	EventTextStart
	// EventTextDelta 表示文本增量，Text 字段包含增量内容。
	EventTextDelta
	// EventTextEnd 表示文本内容块结束。
	EventTextEnd

	// EventThinkingStart 表示思考内容块开始（Claude 等模型支持 extended thinking）。
	EventThinkingStart
	// EventThinkingDelta 表示思考增量。
	EventThinkingDelta
	// EventThinkingEnd 表示思考内容块结束。
	EventThinkingEnd

	// EventToolCallStart 表示工具调用开始。
	EventToolCallStart
	// EventToolCallDelta 表示工具调用参数的增量内容。
	EventToolCallDelta
	// EventToolCallEnd 表示工具调用结束，TC 包含完整参数。
	EventToolCallEnd

	// EventDone 表示整个流式响应正常结束。
	EventDone
	// EventError 表示流式响应出错，Err 字段包含错误信息。
	EventError
)

// Event 是流式响应中的单个事件。
//
// 流结束时 channel 被关闭，无需通过 EventDone 标记。
// Index 用于区分多个同类型块混排（如同时有文本和工具调用）。
type Event struct {
	Type  EventType
	Text  string    // EventTextDelta / EventThinkingDelta: 增量文本
	Index int       // 内容块序号
	TC    *ToolCall // EventToolCallStart/Delta/End: 工具调用信息
	Usage *Usage    // EventDone: token 用量统计
	Err   error     // EventError: 错误详情
}

// Stream 是流式事件通道，Provider.ChatStream() 的返回值。
//
// 用法: for event := range stream { ... }
// channel 关闭意味着流已结束（正常或异常）。
// EventDone 表示正常结束，EventError 表示异常结束。
type Stream <-chan Event
