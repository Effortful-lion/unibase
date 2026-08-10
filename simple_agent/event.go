// Package simple_agent 提供 Agent 级别的事件系统（比 llmkit 的 types.Event 更高层）。
package simple_agent

import (
	"github.com/Effortful-lion/unibase/llmkit/schema"
	"github.com/Effortful-lion/unibase/llmkit/types"
)

// ────────────────── Agent 层事件（底层 API 使用） ──────────────────

// EventType Agent 事件类型。
type EventType int

const (
	// EventStepStart 表示一轮 LLM 调用开始。
	EventStepStart EventType = iota
	// EventStepEnd 表示一轮 LLM 调用结束（含 Usage）。
	EventStepEnd
	// EventToolCall 表示 Agent 决定调用工具。
	EventToolCall
	// EventToolResult 表示工具执行完成并返回结果。
	EventToolResult
	// EventTextDelta 表示 LLM 文本增量（透传给上层）。
	EventTextDelta
	// EventDone 表示 Agent 运行结束（LLM 给出最终回答）。
	EventDone
	// EventError 表示 Agent 运行出错。
	EventError
)

// Event Agent 运行时产生的事件。
type Event struct {
	Type       EventType
	Text       string           // EventTextDelta: 增量文本
	ToolCall   *schema.ToolCall // EventToolCall: 工具调用信息
	ToolResult string           // EventToolResult: 工具执行结果
	Usage      *types.Usage     // EventStepEnd: token 用量统计
	Step       int              // 当前步骤编号（从 1 开始）
	Err        error            // EventError: 错误详情
}

// Stream Agent 事件通道。
// 用法: for event := range stream { ... }
type Stream <-chan Event

// ────────────────── 高层流式事件（AskStream 系列使用） ──────────────────

// StreamEventType 高层流式事件类型。
type StreamEventType int

const (
	// StreamEventStepStart 表示一轮 LLM 调用开始。
	StreamEventStepStart StreamEventType = iota
	// StreamEventStepEnd 表示一轮 LLM 调用结束（含 Usage）。
	StreamEventStepEnd
	// StreamEventTextDelta 表示 LLM 文本增量。
	StreamEventTextDelta
	// StreamEventToolCall 表示 Agent 决定调用工具。
	StreamEventToolCall
	// StreamEventToolResult 表示工具执行完成并返回结果。
	StreamEventToolResult
	// StreamEventDone 表示对话结束。
	StreamEventDone
	// StreamEventError 表示出错。
	StreamEventError
)

// StreamEvent 高层流式事件，由 AskStream 系列方法返回。
type StreamEvent struct {
	Type       StreamEventType
	Text       string           // StreamEventTextDelta: 增量文本
	ToolCall   *schema.ToolCall // StreamEventToolCall: 工具调用信息
	ToolResult string           // StreamEventToolResult: 工具执行结果
	Usage      *types.Usage     // StreamEventDone: token 用量
	Err        error            // StreamEventError: 错误详情
}

// StreamChannel 高层流式事件通道。
// 用法: for evt := range ch { ... }
type StreamChannel <-chan StreamEvent

// ────────────────── 共享类型 ──────────────────

// ToolResult 是工具调用的结果，由 AskWithConfig / AskWithTools 返回。
type ToolResult struct {
	Name      string // 工具名称
	Arguments string // 调用参数（JSON）
	Result    string // 执行结果
}
