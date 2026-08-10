// Package openai 提供了 OpenAI 兼容的 Provider 实现。
package openai

import (
	"context"

	"net/http"

	"github.com/Effortful-lion/unibase/llmkit/provider"
	"github.com/Effortful-lion/unibase/llmkit/types"
)

func init() {
	provider.Register("openai", func(opts *provider.ProviderOptions) (provider.Provider, error) {
		return New(opts)
	})
}

// Provider 是 OpenAI 兼容的 LLM Provider。
type Provider struct {
	model   string
	apiKey  string
	baseURL string
	client  *http.Client
	headers map[string]string
	extra   map[string]any
}

// New 创建 OpenAI Provider。
func New(opts *provider.ProviderOptions) (*Provider, error) {
	if opts == nil {
		return nil, types.NewError(types.ErrCodeInvalidRequest, "", "options is nil")
	}
	p := &Provider{
		model:   opts.ModelName,
		apiKey:  opts.APIKey,
		baseURL: opts.BaseURL,
		client:  http.DefaultClient,
	}
	if p.baseURL == "" {
		p.baseURL = "https://api.openai.com/v1"
	}
	if opts.Headers != nil {
		p.headers = opts.Headers
	}
	if opts.Extra != nil {
		p.extra = opts.Extra
	}
	return p, nil
}

func (p *Provider) Name() string { return "openai" }

func (p *Provider) Chat(ctx context.Context, opts *provider.ProviderOptions, messages []types.Message) (*provider.Response, error) {
	return nil, types.NewError(types.ErrCodeNotImplemented, "openai", "Chat not yet implemented")
}

func (p *Provider) ChatStream(ctx context.Context, opts *provider.ProviderOptions, messages []types.Message) (<-chan types.StreamChunk, error) {
	return nil, types.NewError(types.ErrCodeNotImplemented, "openai", "ChatStream not yet implemented")
}
