// Example 演示 simple_agent 的基本用法。
//
// 运行方式：
//
//	go run ./example
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Effortful-lion/unibase/llmkit/provider"
	"github.com/Effortful-lion/unibase/llmkit/schema"
	"github.com/Effortful-lion/unibase/llmkit/types"
	"github.com/Effortful-lion/unibase/simple_agent"
)

// mockProvider 是一个模拟的 LLM Provider，用于演示。
// 它会根据用户输入决定是否调用工具。
type mockProvider struct {
	modelID string
}

func (p *mockProvider) Name() string { return "mock" }
func (p *mockProvider) Models() []provider.ModelInfo {
	return []provider.ModelInfo{{ID: p.modelID, Name: "Mock Model", ProviderID: "mock"}}
}
func (p *mockProvider) Model(modelID string) (provider.Model, error) {
	if modelID == p.modelID {
		return &mockModel{provider: p, id: modelID}, nil
	}
	return nil, types.NewError(types.ErrCodeInvalidRequest, "mock", "unknown model: "+modelID)
}
func (p *mockProvider) Chat(ctx context.Context, messages []types.Message, opts *provider.ChatOptions) (*provider.Response, error) {
	m, err := p.Model(p.modelID)
	if err != nil {
		return nil, err
	}
	return m.Chat(ctx, messages, opts)
}
func (p *mockProvider) ChatStream(ctx context.Context, messages []types.Message, opts *provider.ChatOptions) (<-chan types.Event, error) {
	m, err := p.Model(p.modelID)
	if err != nil {
		return nil, err
	}
	return m.ChatStream(ctx, messages, opts)
}

type mockModel struct {
	provider *mockProvider
	id       string
}

func (m *mockModel) Info() provider.ModelInfo { return m.provider.Models()[0] }

// Chat 非流式：直接返回完整响应
func (m *mockModel) Chat(ctx context.Context, messages []types.Message, opts *provider.ChatOptions) (*provider.Response, error) {
	lastUserMsg := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == types.RoleUser {
			lastUserMsg = messages[i].Content
			break
		}
	}

	if contains(lastUserMsg, "天气") {
		return &provider.Response{
			Choices: []provider.Choice{{
				Message: types.Message{
					Role:    types.RoleAssistant,
					Content: "",
					Parts: []types.ContentBlock{{
						Type: types.BlockToolCall,
						ToolCall: &types.ToolCall{
							ID:        "call_001",
							Name:      "get_weather",
							Arguments: `{"city":"北京","unit":"celsius"}`,
						},
					}},
				},
			}},
		}, nil
	}

	return &provider.Response{
		Choices: []provider.Choice{{
			Message: types.NewAssistantMessage("你好！我是 Mock 模型。" + lastUserMsg),
		}},
	}, nil
}

// ChatStream 流式：模拟文本增量 + 工具调用
func (m *mockModel) ChatStream(ctx context.Context, messages []types.Message, opts *provider.ChatOptions) (<-chan types.Event, error) {
	ch := make(chan types.Event, 4)

	go func() {
		defer close(ch)

		lastUserMsg := ""
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == types.RoleUser {
				lastUserMsg = messages[i].Content
				break
			}
		}

		// 检查是否有工具结果消息（说明上一轮已执行过工具，现在给出最终回答）
		hasToolResult := false
		for _, msg := range messages {
			if msg.Role == types.RoleTool {
				hasToolResult = true
				break
			}
		}

		if hasToolResult {
			// 工具结果已注入，给出最终回答
			ch <- types.Event{Type: types.EventTextDelta, Text: "根据查询结果，"}
			time.Sleep(50 * time.Millisecond)
			ch <- types.Event{Type: types.EventTextDelta, Text: "北京今天晴，气温 25°C。"}
			ch <- types.Event{Type: types.EventDone, Usage: &types.Usage{TotalTokens: 42}}
			return
		}

		if contains(lastUserMsg, "天气") {
			// 先输出一点文字
			ch <- types.Event{Type: types.EventTextDelta, Text: "我来帮你查询天气，"}
			time.Sleep(30 * time.Millisecond)

			// 发起工具调用
			ch <- types.Event{
				Type:  types.EventToolCallStart,
				Index: 0,
				TC:    &types.ToolCall{ID: "call_001", Name: "get_weather"},
			}
			ch <- types.Event{
				Type:  types.EventToolCallDelta,
				Index: 0,
				TC:    &types.ToolCall{Arguments: `{"city":"北京"`},
			}
			ch <- types.Event{
				Type:  types.EventToolCallDelta,
				Index: 0,
				TC:    &types.ToolCall{Arguments: `,"unit":"celsius"`},
			}
			ch <- types.Event{
				Type:  types.EventToolCallEnd,
				Index: 0,
				TC:    &types.ToolCall{ID: "call_001", Name: "get_weather", Arguments: `{"city":"北京","unit":"celsius"}`},
			}
			ch <- types.Event{Type: types.EventDone}
			return
		}

		// 普通对话
		response := "你好！我是 Mock 模型。" + lastUserMsg
		for _, r := range response {
			ch <- types.Event{Type: types.EventTextDelta, Text: string(r)}
			time.Sleep(20 * time.Millisecond)
		}
		ch <- types.Event{Type: types.EventDone, Usage: &types.Usage{TotalTokens: 10}}
	}()

	return ch, nil
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// weatherTool 是一个模拟的天气查询工具。
type weatherTool struct{}

func (w *weatherTool) Name() string { return "get_weather" }
func (w *weatherTool) Definition() schema.ToolInfo {
	return schema.ToolInfo{
		Name:        "get_weather",
		Description: "查询指定城市的天气信息",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"city"},
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "城市名称，如 北京、上海",
				},
				"unit": map[string]any{
					"type":        "string",
					"description": "温度单位：celsius 或 fahrenheit",
					"enum":        []string{"celsius", "fahrenheit"},
				},
			},
		},
	}
}
func (w *weatherTool) Execute(ctx context.Context, args string) (string, error) {
	return "{\"city\":\"北京\",\"temperature\":25,\"unit\":\"celsius\",\"condition\":\"晴\"}", nil
}

func main() {
	ctx := context.Background()

	fmt.Println("=== 示例 1：简单对话 ===")
	p := &mockProvider{modelID: "mock-v1"}
	ag := simple_agent.New(simple_agent.Config{
		Provider:     p,
		Model:        "mock-v1",
		SystemPrompt: "你是一个有帮助的助手。",
	})

	for evt := range ag.Run(ctx, "你好，请介绍一下你自己") {
		switch evt.Type {
		case simple_agent.EventTextDelta:
			fmt.Print(evt.Text)
		case simple_agent.EventStepStart:
			fmt.Printf("\n[步骤 %d 开始]\n", evt.Step)
		case simple_agent.EventStepEnd:
			fmt.Printf("\n[步骤 %d 结束]\n", evt.Step)
		case simple_agent.EventDone:
			fmt.Println("\n[完成]")
		case simple_agent.EventError:
			fmt.Printf("\n[错误] %v\n", evt.Err)
		}
	}

	fmt.Println("\n=== 示例 2：带工具的对话 ===")
	ag2 := simple_agent.New(simple_agent.Config{
		Provider:     p,
		Model:        "mock-v1",
		SystemPrompt: "你是一个有帮助的助手。",
		Tools:        []simple_agent.Tool{&weatherTool{}},
	})

	for evt := range ag2.Run(ctx, "北京今天天气怎么样？") {
		switch evt.Type {
		case simple_agent.EventStepStart:
			fmt.Printf("\n[步骤 %d 开始]\n", evt.Step)
		case simple_agent.EventStepEnd:
			fmt.Printf("[步骤 %d 结束]\n", evt.Step)
		case simple_agent.EventTextDelta:
			fmt.Print(evt.Text)
		case simple_agent.EventToolCall:
			fmt.Printf("\n[调用工具] %s 参数: %s\n", evt.ToolCall.Name, evt.ToolCall.Arguments)
		case simple_agent.EventToolResult:
			fmt.Printf("[工具结果] %s\n", evt.ToolResult)
		case simple_agent.EventDone:
			fmt.Println("\n[完成]")
		case simple_agent.EventError:
			fmt.Printf("\n[错误] %v\n", evt.Err)
		}
	}

	fmt.Println("\n=== 示例 3：Continue 继续对话 ===")
	ag3 := simple_agent.New(simple_agent.Config{
		Provider:     p,
		Model:        "mock-v1",
		SystemPrompt: "你是一个有帮助的助手。",
		Tools:        []simple_agent.Tool{&weatherTool{}},
	})

	// 第一轮
	for evt := range ag3.Run(ctx, "北京今天天气怎么样？") {
		switch evt.Type {
		case simple_agent.EventTextDelta:
			fmt.Print(evt.Text)
		case simple_agent.EventToolCall:
			fmt.Printf("\n[调用工具] %s\n", evt.ToolCall.Name)
		case simple_agent.EventToolResult:
			fmt.Printf("[工具结果] %s\n", evt.ToolResult)
		case simple_agent.EventDone:
			fmt.Println("\n[第一轮完成]")
		}
	}

	// 第二轮：基于上一轮继续
	fmt.Println("\n--- 继续对话 ---")
	for evt := range ag3.Continue(ctx, "那明天呢？") {
		switch evt.Type {
		case simple_agent.EventTextDelta:
			fmt.Print(evt.Text)
		case simple_agent.EventDone:
			fmt.Println("\n[第二轮完成]")
		}
	}

	// 查看历史
	fmt.Printf("\n[对话历史共 %d 条消息]\n", len(ag3.Messages()))

	fmt.Println("\n=== 示例 4：高层同步 API — Ask ===")
	result, err := simple_agent.Ask(ctx, p, "mock-v1", "你好，请用一句话介绍你自己")
	if err != nil {
		fmt.Printf("[错误] %v\n", err)
	} else {
		fmt.Printf("[回答] %s\n", result)
	}

	fmt.Println("\n=== 示例 5：高层同步 API — AskWithTools ===")
	result, toolResults, err := simple_agent.AskWithTools(ctx, p, "mock-v1",
		"北京今天天气", []simple_agent.Tool{&weatherTool{}})
	if err != nil {
		fmt.Printf("[错误] %v\n", err)
	} else {
		fmt.Printf("[回答] %s\n", result)
		if len(toolResults) > 0 {
			for _, tr := range toolResults {
				fmt.Printf("[工具 %s 结果] %s\n", tr.Name, tr.Result)
			}
		}
	}

	fmt.Println("\n=== 示例 6：高层流式 API — AskStream ===")
	streamCh, err := simple_agent.AskStream(ctx, p, "mock-v1", "你好，请用一句话介绍你自己")
	if err != nil {
		fmt.Printf("[错误] %v\n", err)
	} else {
		for evt := range streamCh {
			switch evt.Type {
			case simple_agent.StreamEventTextDelta:
				fmt.Print(evt.Text)
			case simple_agent.StreamEventDone:
				fmt.Println("\n[流式完成]")
			case simple_agent.StreamEventError:
				fmt.Printf("\n[错误] %v\n", evt.Err)
			}
		}
	}

	fmt.Println("\n=== 示例 7：高层流式 API — AskStreamWithTools ===")
	streamCh, err = simple_agent.AskStreamWithTools(ctx, p, "mock-v1",
		"北京今天天气", []simple_agent.Tool{&weatherTool{}})
	if err != nil {
		fmt.Printf("[错误] %v\n", err)
	} else {
		for evt := range streamCh {
			switch evt.Type {
			case simple_agent.StreamEventTextDelta:
				fmt.Print(evt.Text)
			case simple_agent.StreamEventToolCall:
				fmt.Printf("\n[调用工具] %s\n", evt.ToolCall.Name)
			case simple_agent.StreamEventToolResult:
				fmt.Printf("[工具结果] %s\n", evt.ToolResult)
			case simple_agent.StreamEventDone:
				fmt.Println("\n[流式完成]")
			case simple_agent.StreamEventError:
				fmt.Printf("\n[错误] %v\n", evt.Err)
			}
		}
	}

	fmt.Println("\n=== 示例 8：AskWithConfig 全配置 ===")
	result, toolResults, err = simple_agent.AskWithConfig(ctx, p, simple_agent.AskConfig{
		Model:        "mock-v1",
		Prompt:       "上海今天天气怎么样？",
		SystemPrompt: "你是一个专业的天气助手，请用简洁的语言回答。",
		Tools:        []simple_agent.Tool{&weatherTool{}},
		Temperature:  0.7,
		MaxSteps:     5,
	})
	if err != nil {
		fmt.Printf("[错误] %v\n", err)
	} else {
		fmt.Printf("[回答] %s\n", result)
		if len(toolResults) > 0 {
			for _, tr := range toolResults {
				fmt.Printf("[工具 %s] 参数=%s 结果=%s\n", tr.Name, tr.Arguments, tr.Result)
			}
		}
	}
}
