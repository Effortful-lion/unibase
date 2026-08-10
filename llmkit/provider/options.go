package provider

import "github.com/Effortful-lion/unibase/llmkit/schema"

// ────────────────── Provider 创建选项 ──────────────────

// ProviderOption 配置 Provider 创建参数。
type ProviderOption func(*ProviderOptions)

// ProviderOptions 是创建 Provider 时的配置。
type ProviderOptions struct {
	APIKey  string
	BaseURL string
	Headers map[string]string
	Extra   map[string]any
}

func WithAPIKey(key string) ProviderOption {
	return func(o *ProviderOptions) { o.APIKey = key }
}

func WithBaseURL(url string) ProviderOption {
	return func(o *ProviderOptions) { o.BaseURL = url }
}

func WithHeaders(headers map[string]string) ProviderOption {
	return func(o *ProviderOptions) {
		if o.Headers == nil {
			o.Headers = make(map[string]string)
		}
		for k, v := range headers {
			o.Headers[k] = v
		}
	}
}

func WithExtra(extra map[string]any) ProviderOption {
	return func(o *ProviderOptions) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		for k, v := range extra {
			o.Extra[k] = v
		}
	}
}

// ────────────────── 单次调用选项 ──────────────────

// ChatOptions 是单次 Chat 调用的配置。
// 指针字段为 nil 表示"未设置"，由 Provider 使用模型默认值。
type ChatOptions struct {
	Model       *string
	Temperature *float32
	MaxTokens   *int
	TopP        *float32
	Stop        []string
	Tools       []schema.ToolInfo
	ToolChoice  *schema.ToolChoice
	Stream      bool
}

// CallOption 是 ChatOptions 的函数式配置选项。
type CallOption func(*ChatOptions)

// WithModel 设置要使用的模型 ID。
func WithModel(name string) CallOption {
	return func(o *ChatOptions) { o.Model = &name }
}

// WithTemperature 设置采样温度 (0~2)。
func WithTemperature(t float32) CallOption {
	return func(o *ChatOptions) { o.Temperature = &t }
}

// WithMaxTokens 设置最大输出 tokens。
func WithMaxTokens(n int) CallOption {
	return func(o *ChatOptions) { o.MaxTokens = &n }
}

// WithTopP 设置 nucleus sampling 参数。
func WithTopP(p float32) CallOption {
	return func(o *ChatOptions) { o.TopP = &p }
}

// WithStop 设置停止序列。
func WithStop(words []string) CallOption {
	return func(o *ChatOptions) { o.Stop = words }
}

// WithTools 设置可用工具列表。
func WithTools(tools []schema.ToolInfo) CallOption {
	return func(o *ChatOptions) { o.Tools = tools }
}

// WithToolChoice 设置工具调用策略。
func WithToolChoice(choice schema.ToolChoice) CallOption {
	return func(o *ChatOptions) { o.ToolChoice = &choice }
}

// WithStream 启用流式输出。
func WithStream(enable bool) CallOption {
	return func(o *ChatOptions) { o.Stream = enable }
}
