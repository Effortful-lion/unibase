// Package anthropic 提供了 Anthropic Claude 的 Provider 实现。
package anthropic

import (
	"context"

	"github.com/Effortful-lion/unibase/llmkit/provider"
	"github.com/Effortful-lion/unibase/llmkit/types"
)

func init() {
	provider.Register("anthropic", func(opts *provider.ProviderOptions) (provider.Provider, error) {
		return New(opts)
	})
}

// Provider 是 Anthropic Claude 的 LLM Provider。
type Provider struct {
	model   string
	apiKey  string
	baseURL string
}

// New 创建 Anthropic Provider。
func New(opts *provider.ProviderOptions) (*Provider, error) {
	if opts == nil {
		return nil, types.NewError(types.ErrCodeInvalidRequest, "", "options is nil")
	}
	p := &Provider{
		model:   opts.ModelName,
		apiKey:  opts.APIKey,
		baseURL: opts.BaseURL,
	}
	if p.baseURL == "" {
		p.baseURL = "https://api.anthropic.com/v1"
	}
	return p, nil
}

func (p *Provider) Name() string { return "anthropic" }

func (p *Provider) Chat(ctx context.Context, opts *provider.ProviderOptions, messages []types.Message) (*provider.Response, error) {
	return nil, types.NewError(types.ErrCodeNotImplemented, "anthropic", "Chat not yet implemented")
}

func (p *Provider) ChatStream(ctx context.Context, opts *provider.ProviderOptions, messages []types.Message) (<-chan types.StreamChunk, error) {
	return nil, types.NewError(types.ErrCodeNotImplemented, "anthropic", "ChatStream not yet implemented")
}
