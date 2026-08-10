// Package provider 提供了 LLM Provider 的统一接口和注册机制。
package provider

import (
	"context"
	"sync"

	"github.com/Effortful-lion/unibase/llmkit/types"
)

// Provider 是 LLM 提供商的统一接口。
type Provider interface {
	// Name 返回提供商名称。
	Name() string
	// Chat 发起一次非流式对话。
	Chat(ctx context.Context, opts *ProviderOptions, messages []types.Message) (*Response, error)
	// ChatStream 发起一次流式对话。
	ChatStream(ctx context.Context, opts *ProviderOptions, messages []types.Message) (<-chan types.StreamChunk, error)
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

// Response 是对话响应。
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
