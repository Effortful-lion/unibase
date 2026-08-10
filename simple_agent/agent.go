// Package simple_agent 提供了一个基于 llmkit 的通用 Agent 运行时。
//
// Agent 核心职责：
//   - 接收用户输入，管理对话历史
//   - 调用 LLM（通过 llmkit Provider），收集文本响应和工具调用
//   - 自动执行工具调用并注入结果，往复直到 LLM 给出最终回答
//
// 使用示例：
//
//	// ── 底层 API：完全控制事件流 ──
//	p := openai.New(openai.Config{APIKey: "sk-..."})
//	ag := simple_agent.New(simple_agent.Config{
//	    Provider: p,
//	    Model:    "gpt-4o",
//	    Tools:    []simple_agent.Tool{weatherTool},
//	})
//	for evt := range ag.Run(ctx, "北京今天天气") {
//	    switch evt.Type {
//	    case simple_agent.EventTextDelta:
//	        fmt.Print(evt.Text)
//	    case simple_agent.EventToolCall:
//	        fmt.Println("[调用]", evt.ToolCall.Name)
//	    case simple_agent.EventDone:
//	        fmt.Println("\n完成")
//	    }
//	}
//
//	// ── 高层 API：一个方法搞定 ──
//	// 同步
//	result, toolResults, err := simple_agent.AskWithTools(ctx, p, "gpt-4o",
//	    "北京今天天气", []simple_agent.Tool{weatherTool})
//	// 流式
//	for evt := range simple_agent.AskStream(ctx, p, "gpt-4o", "你好") {
//	    fmt.Print(evt.Text)
//	}
package simple_agent

import (
	"context"

	"github.com/Effortful-lion/unibase/llmkit/provider"
	"github.com/Effortful-lion/unibase/llmkit/schema"
	"github.com/Effortful-lion/unibase/llmkit/types"
)

// Config Agent 配置。
type Config struct {
	Provider     provider.Provider
	Model        string
	SystemPrompt string
	Tools        []Tool
	MaxSteps     int     // 最大工具调用循环轮数，≤0 时使用默认值 10
	Temperature  float64 // LLM 采样温度，0 表示使用模型默认值
	MaxTokens    int     // LLM 最大输出 tokens，0 表示不限制
}

// Agent 对话型 Agent，管理 LLM ↔ 工具的自动循环。
type Agent struct {
	cfg      Config
	toolMap  map[string]Tool
	messages []types.Message
}

const defaultMaxSteps = 10

// New 创建 Agent。
func New(cfg Config) *Agent {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = defaultMaxSteps
	}

	tm := make(map[string]Tool, len(cfg.Tools))
	for _, t := range cfg.Tools {
		tm[t.Name()] = t
	}

	a := &Agent{
		cfg:     cfg,
		toolMap: tm,
	}

	if cfg.SystemPrompt != "" {
		a.messages = append(a.messages, types.Message{
			Role:    types.RoleSystem,
			Content: cfg.SystemPrompt,
		})
	}

	return a
}

// Run 启动一轮 Agent 对话循环。
//
// 返回 Stream 事件通道。goroutine 中执行 LLM 调用和工具循环，
// 完成后自动关闭通道。userInput 为空时不追加任何消息（用于 Continue 场景）。
func (a *Agent) Run(ctx context.Context, userInput string) Stream {
	out := make(chan Event, 8)

	if userInput != "" {
		a.messages = append(a.messages, types.Message{
			Role:    types.RoleUser,
			Content: userInput,
		})
	}

	go func() {
		defer close(out)
		a.runLoop(ctx, out)
	}()

	return out
}

// Continue 在现有对话基础上继续，可选注入额外上下文。
//
// extraContext 非空时会被作为一条 user 消息追加到历史中。
// 不追加新的空 user 消息，直接让 LLM 基于当前历史继续生成。
func (a *Agent) Continue(ctx context.Context, extraContext string) Stream {
	if extraContext != "" {
		a.messages = append(a.messages, types.Message{
			Role:    types.RoleUser,
			Content: extraContext,
		})
	}
	return a.Run(ctx, "")
}

// Reset 清空对话历史，保留 system prompt，开始新对话。
func (a *Agent) Reset() {
	a.messages = nil
	if a.cfg.SystemPrompt != "" {
		a.messages = append(a.messages, types.Message{
			Role:    types.RoleSystem,
			Content: a.cfg.SystemPrompt,
		})
	}
}

// Messages 返回当前对话历史快照（只读）。
func (a *Agent) Messages() []types.Message {
	cp := make([]types.Message, len(a.messages))
	copy(cp, a.messages)
	return cp
}

// buildChatOptions 从 Agent 配置构建 ChatOptions。
func (a *Agent) buildChatOptions() *provider.ChatOptions {
	opts := &provider.ChatOptions{}
	if a.cfg.Temperature > 0 {
		t := float32(a.cfg.Temperature)
		opts.Temperature = &t
	}
	if a.cfg.MaxTokens > 0 {
		opts.MaxTokens = &a.cfg.MaxTokens
	}
	if len(a.cfg.Tools) > 0 {
		tools := make([]schema.ToolInfo, len(a.cfg.Tools))
		for i, t := range a.cfg.Tools {
			tools[i] = t.Definition()
		}
		opts.Tools = tools
	}
	return opts
}

// resolveModel 获取当前调用使用的模型。
func (a *Agent) resolveModel() (provider.Model, error) {
	m, err := a.cfg.Provider.Model(a.cfg.Model)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// runLoop Agent 对话循环核心。
func (a *Agent) runLoop(ctx context.Context, out chan<- Event) {
	for step := 1; step <= a.cfg.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			out <- Event{Type: EventError, Step: step, Err: err}
			return
		}

		out <- Event{Type: EventStepStart, Step: step}

		// 获取模型实例
		model, err := a.resolveModel()
		if err != nil {
			out <- Event{Type: EventError, Step: step, Err: err}
			return
		}

		// 调用 LLM 流式接口
		stream, err := model.ChatStream(ctx, a.messages, a.buildChatOptions())
		if err != nil {
			out <- Event{Type: EventError, Step: step, Err: err}
			return
		}

		// 消费流
		var lastUsage *types.Usage
		var textParts []string
		toolCallAcc := make(map[int]*schema.ToolCall)
		var toolCallsOrdered []*schema.ToolCall

		for evt := range stream {
			switch evt.Type {
			case types.EventTextDelta:
				textParts = append(textParts, evt.Text)
				out <- Event{Type: EventTextDelta, Text: evt.Text, Step: step}
			case types.EventThinkingStart, types.EventThinkingDelta, types.EventThinkingEnd:
				// 思考内容暂不暴露给上层
			case types.EventToolCallStart:
				tc := &schema.ToolCall{
					ID:   evt.TC.ID,
					Name: evt.TC.Name,
				}
				toolCallAcc[evt.Index] = tc
			case types.EventToolCallDelta:
				if tc, ok := toolCallAcc[evt.Index]; ok {
					tc.Arguments += evt.TC.Arguments
				}
			case types.EventToolCallEnd:
				if tc, ok := toolCallAcc[evt.Index]; ok {
					toolCallsOrdered = append(toolCallsOrdered, tc)
					delete(toolCallAcc, evt.Index)
				}
			case types.EventDone:
				lastUsage = evt.Usage
			case types.EventError:
				out <- Event{Type: EventError, Step: step, Err: evt.Err}
				return
			}
		}

		out <- Event{Type: EventStepEnd, Step: step, Usage: lastUsage}

		// 构建 AssistantMessage
		assistantMsg := types.Message{Role: types.RoleAssistant}
		for _, p := range textParts {
			assistantMsg.Content += p
		}
		for _, tc := range toolCallsOrdered {
			assistantMsg.Parts = append(assistantMsg.Parts, types.ContentBlock{
				Type:     types.BlockToolCall,
				ToolCall: &types.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments},
			})
		}

		// 无工具调用 → 对话结束
		if len(toolCallsOrdered) == 0 {
			a.messages = append(a.messages, assistantMsg)
			out <- Event{Type: EventDone, Step: step}
			return
		}

		// 有工具调用 → 执行并注入结果
		a.messages = append(a.messages, assistantMsg)

		for _, tc := range toolCallsOrdered {
			out <- Event{Type: EventToolCall, Step: step, ToolCall: tc}

			t, ok := a.toolMap[tc.Name]
			if !ok {
				err := &Error{Message: "unknown tool: " + tc.Name, Code: ErrCodeTool}
				out <- Event{Type: EventError, Step: step, Err: err}
				return
			}

			result, execErr := t.Execute(ctx, tc.Arguments)
			if execErr != nil {
				result = "tool execution error: " + execErr.Error()
			}

			out <- Event{Type: EventToolResult, Step: step, ToolResult: result}

			a.messages = append(a.messages, types.Message{
				Role:       types.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			})
		}
	}

	// 超过 MaxSteps
	out <- Event{
		Type: EventError,
		Step: a.cfg.MaxSteps,
		Err:  &Error{Message: "max steps exceeded: agent could not complete within the step limit", Code: ErrCodeMaxSteps},
	}
}

// ────────────────── AskConfig ──────────────────

// AskConfig 是 Ask 系列方法的配置。
type AskConfig struct {
	Model        string
	Prompt       string
	SystemPrompt string
	Tools        []Tool
	Temperature  float64
	MaxTokens    int
	MaxSteps     int
}

// ────────────────── 高层同步 API ──────────────────

// Ask 最简单的同步对话：一个问题，一个答案。
func Ask(ctx context.Context, p provider.Provider, model, prompt string) (string, error) {
	result, _, err := AskWithConfig(ctx, p, AskConfig{Model: model, Prompt: prompt})
	return result, err
}

// AskWithTools 带工具的同步对话。
//
// 返回 (最终回答文本, 工具执行结果列表, error)。
func AskWithTools(ctx context.Context, p provider.Provider, model, prompt string, tools []Tool) (string, []ToolResult, error) {
	result, toolResults, err := AskWithConfig(ctx, p, AskConfig{
		Model:  model,
		Prompt: prompt,
		Tools:  tools,
	})
	return result, toolResults, err
}

// AskWithConfig 全配置同步对话，返回最终回答和工具调用结果。
//
// 使用场景：需要 system prompt、temperature、max_tokens 等精细控制。
// Tools 非空时，Agent 会在必要时自动调用工具并返回结果。
func AskWithConfig(ctx context.Context, p provider.Provider, cfg AskConfig) (string, []ToolResult, error) {
	ag := newAgentFromAskConfig(p, cfg)
	return runSync(ctx, ag, cfg.Prompt)
}

// ────────────────── 高层流式 API ──────────────────

// AskStream 最简单的流式对话，逐 token 消费。
//
// 返回的 channel 会发送 StreamEvent，关闭时对话结束。
// 调用方用 for range 消费，遇到 Done 类型取最终文本。
func AskStream(ctx context.Context, p provider.Provider, model, prompt string) (<-chan StreamEvent, error) {
	return AskStreamWithConfig(ctx, p, AskConfig{Model: model, Prompt: prompt})
}

// AskStreamWithTools 带工具的流式对话。
//
// StreamEvent 中 ToolCall / ToolResult 类型携带工具执行信息。
func AskStreamWithTools(ctx context.Context, p provider.Provider, model, prompt string, tools []Tool) (<-chan StreamEvent, error) {
	return AskStreamWithConfig(ctx, p, AskConfig{
		Model:  model,
		Prompt: prompt,
		Tools:  tools,
	})
}

// AskStreamWithConfig 全配置流式对话。
//
// StreamEvent 类型说明：
//   - TextDelta: LLM 文本增量
//   - ToolCall: Agent 决定调用工具
//   - ToolResult: 工具执行完成
//   - Done: 对话结束
//   - Error: 出错
func AskStreamWithConfig(ctx context.Context, p provider.Provider, cfg AskConfig) (<-chan StreamEvent, error) {
	ag := newAgentFromAskConfig(p, cfg)
	out := make(chan StreamEvent, 8)
	go func() {
		defer close(out)
		for evt := range ag.Run(ctx, cfg.Prompt) {
			switch evt.Type {
			case EventStepStart:
				out <- StreamEvent{Type: StreamEventStepStart}
			case EventStepEnd:
				out <- StreamEvent{Type: StreamEventStepEnd, Usage: evt.Usage}
			case EventTextDelta:
				out <- StreamEvent{Type: StreamEventTextDelta, Text: evt.Text}
			case EventToolCall:
				out <- StreamEvent{Type: StreamEventToolCall, ToolCall: evt.ToolCall}
			case EventToolResult:
				out <- StreamEvent{Type: StreamEventToolResult, ToolResult: evt.ToolResult}
			case EventDone:
				out <- StreamEvent{Type: StreamEventDone}
			case EventError:
				out <- StreamEvent{Type: StreamEventError, Err: evt.Err}
			}
		}
	}()
	return out, nil
}

// ────────────────── 内部辅助 ──────────────────

func newAgentFromAskConfig(p provider.Provider, cfg AskConfig) *Agent {
	agentCfg := Config{
		Provider:     p,
		Model:        cfg.Model,
		SystemPrompt: cfg.SystemPrompt,
		Tools:        cfg.Tools,
		MaxSteps:     cfg.MaxSteps,
		Temperature:  cfg.Temperature,
		MaxTokens:    cfg.MaxTokens,
	}
	return New(agentCfg)
}

// runSync 运行 Agent 并同步收集结果。
func runSync(ctx context.Context, ag *Agent, prompt string) (string, []ToolResult, error) {
	var finalText string
	var toolResults []ToolResult

	for evt := range ag.Run(ctx, prompt) {
		switch evt.Type {
		case EventTextDelta:
			finalText += evt.Text
		case EventToolCall:
			toolResults = append(toolResults, ToolResult{
				Name:      evt.ToolCall.Name,
				Arguments: evt.ToolCall.Arguments,
				Result:    evt.ToolResult,
			})
		case EventDone:
			// 完成
		case EventError:
			if evt.Err != nil {
				return "", toolResults, evt.Err
			}
		}
	}

	if len(toolResults) == 0 {
		return finalText, nil, nil
	}
	return finalText, toolResults, nil
}
