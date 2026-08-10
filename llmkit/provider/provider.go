// Package provider 提供了 LLM Provider 的统一接口和注册机制。
package provider

import (
	"context"
	"sync"

	"github.com/Effortful-lion/unibase/llmkit/types"
)

// Provider 是 LLM 提供商的统一接口。
//
// 每个 Provider 管理一组模型，负责认证和流式对话。
// 简单用法直接调用 Chat/ChatStream；高级用法通过 Model() 获取模型实例。
type Provider interface {
	// Name 返回提供商名称，如 "openai"、"anthropic"。
	Name() string

	// Models 返回该提供商所有可用模型的元信息列表。
	Models() []ModelInfo

	// Model 获取指定 ID 的模型实例，用于模型级配置。
	Model(modelID string) (Model, error)

	// Chat 发起一次非流式对话。
	Chat(ctx context.Context, messages []types.Message, opts *ChatOptions) (*Response, error)

	// ChatStream 发起一次流式对话，返回事件通道。
	// channel 关闭意味着流结束（正常或异常）。
	ChatStream(ctx context.Context, messages []types.Message, opts *ChatOptions) (<-chan types.Event, error)
}

// Model 是绑定到具体 Provider 的模型实例，提供模型级的 Chat 能力。
type Model interface {
	// Info 返回模型元信息。
	Info() ModelInfo

	// Chat 使用该模型进行非流式对话。
	Chat(ctx context.Context, messages []types.Message, opts *ChatOptions) (*Response, error)

	// ChatStream 使用该模型进行流式对话。
	ChatStream(ctx context.Context, messages []types.Message, opts *ChatOptions) (<-chan types.Event, error)
}

// ModelInfo 是模型的元信息。
type ModelInfo struct {
	ID            string // 模型唯一标识，如 "gpt-4o", "claude-sonnet-4-5"
	Name          string // 展示名称
	ProviderID    string // 所属提供商 ID
	ContextWindow int    // 上下文窗口大小（tokens），0 表示未知
	MaxOutput     int    // 最大输出 tokens，0 表示未知
}

// ProviderFactory 创建 Provider 实例。
type ProviderFactory func(opts *ProviderOptions) (Provider, error)

// 注册表
var (
	providersMu sync.RWMutex
	providers   = make(map[string]ProviderFactory)
)

// Register 注册一个 Provider 工厂。
func Register(name string, factory ProviderFactory) {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers[name] = factory
}

// Get 获取已注册的 Provider 工厂。
func Get(name string) (ProviderFactory, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	f, ok := providers[name]
	return f, ok
}

// NewProvider 根据名称创建 Provider 实例。
func NewProvider(name string, opts ...ProviderOption) (Provider, error) {
	providersMu.RLock()
	f, ok := providers[name]
	providersMu.RUnlock()
	if !ok {
		return nil, types.NewError(types.ErrCodeInvalidRequest, "", "unknown provider: "+name)
	}
	options := &ProviderOptions{}
	for _, opt := range opts {
		opt(options)
	}
	return f(options)
}

// Response 是对话响应（非流式）。
type Response struct {
	ID           string
	Model        string
	Choices      []Choice
	Usage        *types.Usage
	FinishReason string
}

// Choice 是响应中的一个候选。
type Choice struct {
	Index        int
	Message      types.Message
	FinishReason string
}
