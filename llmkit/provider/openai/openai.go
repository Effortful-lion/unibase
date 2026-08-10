// Package openai 提供了 OpenAI 兼容协议的 Provider 实现。
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Effortful-lion/unibase/llmkit/provider"
	"github.com/Effortful-lion/unibase/llmkit/types"
)

const defaultBaseURL = "https://api.openai.com/v1"

// Config OpenAI Provider 配置。
type Config struct {
	APIKey  string
	BaseURL string
}

func init() {
	provider.Register("openai", func(opts *provider.ProviderOptions) (provider.Provider, error) {
		return New(opts)
	})
}

// Provider 是 OpenAI 兼容的 LLM Provider。
type Provider struct {
	cfg    Config
	models []provider.ModelInfo
}

// Model 是 OpenAI 模型的实现。
type Model struct {
	p    *Provider
	info provider.ModelInfo
}

var knownModels = []provider.ModelInfo{
	{ID: "gpt-4o", Name: "GPT-4o", ProviderID: "openai", ContextWindow: 128000, MaxOutput: 16384},
	{ID: "gpt-4o-mini", Name: "GPT-4o Mini", ProviderID: "openai", ContextWindow: 128000, MaxOutput: 16384},
	{ID: "o1-preview", Name: "o1 Preview", ProviderID: "openai", ContextWindow: 128000, MaxOutput: 32768},
}

// New 创建 OpenAI Provider。
func New(opts *provider.ProviderOptions) (*Provider, error) {
	if opts == nil {
		return nil, types.NewError(types.ErrCodeInvalidRequest, "", "options is nil")
	}
	cfg := Config{
		APIKey:  opts.APIKey,
		BaseURL: opts.BaseURL,
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return &Provider{
		cfg:    cfg,
		models: knownModels,
	}, nil
}

// Name 返回提供商名称。
func (p *Provider) Name() string { return "openai" }

// Models 返回所有可用模型。
func (p *Provider) Models() []provider.ModelInfo { return p.models }

// Model 获取指定模型。
func (p *Provider) Model(modelID string) (provider.Model, error) {
	for _, m := range p.models {
		if m.ID == modelID {
			return &Model{p: p, info: m}, nil
		}
	}
	return nil, types.NewError(types.ErrCodeInvalidRequest, "openai", "unknown model: "+modelID)
}

// Chat 发起非流式对话。
func (p *Provider) Chat(ctx context.Context, messages []types.Message, opts *provider.ChatOptions) (*provider.Response, error) {
	m, err := p.resolveModel(opts)
	if err != nil {
		return nil, err
	}
	return m.Chat(ctx, messages, opts)
}

// ChatStream 发起流式对话。
func (p *Provider) ChatStream(ctx context.Context, messages []types.Message, opts *provider.ChatOptions) (<-chan types.Event, error) {
	m, err := p.resolveModel(opts)
	if err != nil {
		return nil, err
	}
	return m.ChatStream(ctx, messages, opts)
}

// resolveModel 根据 ChatOptions 或默认模型获取 Model。
func (p *Provider) resolveModel(opts *provider.ChatOptions) (*Model, error) {
	if opts != nil && opts.Model != nil {
		m, err := p.Model(*opts.Model)
		if err != nil {
			return nil, err
		}
		return m.(*Model), nil
	}
	return nil, types.NewError(types.ErrCodeInvalidRequest, "openai", "model not specified")
}

// Info 返回模型元信息。
func (m *Model) Info() provider.ModelInfo { return m.info }

// Chat 非流式对话。
func (m *Model) Chat(ctx context.Context, messages []types.Message, opts *provider.ChatOptions) (*provider.Response, error) {
	return m.chat(ctx, messages, opts)
}

// ChatStream 流式对话。
func (m *Model) ChatStream(ctx context.Context, messages []types.Message, opts *provider.ChatOptions) (<-chan types.Event, error) {
	out := make(chan types.Event, 16)
	go func() {
		defer close(out)
		m.chatStream(ctx, messages, opts, out)
	}()
	return out, nil
}

// chatStream 执行流式对话。
func (m *Model) chatStream(ctx context.Context, messages []types.Message, opts *provider.ChatOptions, out chan<- types.Event) {
	reqBody, err := m.buildRequest(messages, opts, true)
	if err != nil {
		out <- types.Event{Type: types.EventError, Err: fmt.Errorf("build request: %w", err)}
		return
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		out <- types.Event{Type: types.EventError, Err: fmt.Errorf("marshal request: %w", err)}
		return
	}

	url := m.p.cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		out <- types.Event{Type: types.EventError, Err: fmt.Errorf("create request: %w", err)}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+m.p.cfg.APIKey)

	httpClient := http.DefaultClient
	resp, err := httpClient.Do(req)
	if err != nil {
		out <- types.Event{Type: types.EventError, Err: fmt.Errorf("http request: %w", err)}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		out <- types.Event{Type: types.EventError, Err: fmt.Errorf("chat failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(b)))}
		return
	}

	out <- types.Event{Type: types.EventStart}
	m.parseSSE(resp.Body, out)
}

// ────────────────── 请求构建 ──────────────────

type openaiChatMessage struct {
	Role       string              `json:"role"`
	Content    string              `json:"content,omitempty"`
	ToolCalls  []openaiToolCallMsg `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}

type openaiToolCallMsg struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openaiToolCallFunc `json:"function"`
}

type openaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiToolDef struct {
	Type     string            `json:"type"`
	Function openaiToolDefFunc `json:"function"`
}

type openaiToolDefFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openaiChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openaiChatMessage `json:"messages"`
	Stream      bool                `json:"stream"`
	Tools       []openaiToolDef     `json:"tools,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
}

// buildRequest 构造 OpenAI Chat Completions 请求体。
func (m *Model) buildRequest(messages []types.Message, opts *provider.ChatOptions, stream bool) (openaiChatRequest, error) {
	msgs := make([]openaiChatMessage, 0)

	for _, msg := range messages {
		switch msg.Role {
		case types.RoleUser, types.RoleSystem:
			msgs = append(msgs, openaiChatMessage{
				Role:    string(msg.Role),
				Content: msg.Content,
			})
		case types.RoleAssistant:
			cm := openaiChatMessage{Role: "assistant", Content: msg.Content}
			if len(msg.Parts) > 0 {
				tcs := buildToolCallMsgs(msg.Parts)
				if len(tcs) > 0 {
					cm.ToolCalls = tcs
				}
			}
			msgs = append(msgs, cm)
		case types.RoleTool:
			msgs = append(msgs, openaiChatMessage{
				Role:       "tool",
				ToolCallID: msg.ToolCallID,
				Content:    msg.Content,
			})
		}
	}

	req := openaiChatRequest{
		Model:    m.info.ID,
		Messages: msgs,
		Stream:   stream,
	}

	if opts != nil {
		if opts.MaxTokens != nil && *opts.MaxTokens > 0 {
			req.MaxTokens = *opts.MaxTokens
		}
		if opts.Temperature != nil && *opts.Temperature > 0 {
			req.Temperature = float64(*opts.Temperature)
		}
		if len(opts.Tools) > 0 {
			tools := make([]openaiToolDef, len(opts.Tools))
			for i, t := range opts.Tools {
				tools[i] = openaiToolDef{
					Type: "function",
					Function: openaiToolDefFunc{
						Name:        t.Name,
						Description: t.Description,
						Parameters:  t.Parameters,
					},
				}
			}
			req.Tools = tools
		}
	}

	return req, nil
}

// ────────────────── 非流式响应解析 ──────────────────

// openaiNonStreamResponse 非流式 Chat Completions 响应。
type openaiNonStreamResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role       string              `json:"role"`
			Content    string              `json:"content"`
			ToolCalls  []openaiToolCallMsg `json:"tool_calls,omitempty"`
			ToolCallID string              `json:"tool_call_id,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// chat 执行非流式对话。
func (m *Model) chat(ctx context.Context, messages []types.Message, opts *provider.ChatOptions) (*provider.Response, error) {
	reqBody, err := m.buildRequest(messages, opts, false)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := m.p.cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.p.cfg.APIKey)

	httpClient := http.DefaultClient
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chat failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var body openaiNonStreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(body.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	choice := body.Choices[0]
	respMsg := types.Message{
		Role:    types.Role(choice.Message.Role),
		Content: choice.Message.Content,
	}

	// 处理工具调用
	if len(choice.Message.ToolCalls) > 0 {
		respMsg.Parts = make([]types.ContentBlock, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			respMsg.Parts = append(respMsg.Parts, types.ContentBlock{
				Type: types.BlockToolCall,
				ToolCall: &types.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}

	return &provider.Response{
		ID:    body.ID,
		Model: body.Model,
		Usage: &types.Usage{
			PromptTokens:     body.Usage.PromptTokens,
			CompletionTokens: body.Usage.CompletionTokens,
			TotalTokens:      body.Usage.TotalTokens,
		},
		FinishReason: choice.FinishReason,
		Choices: []provider.Choice{{
			Index:        choice.Index,
			Message:      respMsg,
			FinishReason: choice.FinishReason,
		}},
	}, nil
}

// buildToolCallMsgs 将 llmkit ContentBlock 转换为 OpenAI tool_calls 格式。
func buildToolCallMsgs(parts []types.ContentBlock) []openaiToolCallMsg {
	var res []openaiToolCallMsg
	for _, b := range parts {
		if b.Type == types.BlockToolCall && b.ToolCall != nil {
			res = append(res, openaiToolCallMsg{
				ID:   b.ToolCall.ID,
				Type: "function",
				Function: openaiToolCallFunc{
					Name:      b.ToolCall.Name,
					Arguments: b.ToolCall.Arguments,
				},
			})
		}
	}
	return res
}

// ────────────────── SSE 解析 ──────────────────

// toolCallAccumulator 跨 chunk 累积工具调用。
type toolCallAccumulator struct {
	tc *types.ToolCall
}

// messageChunk OpenAI SSE 单个 chunk 的 JSON 结构。
type messageChunk struct {
	Choices []struct {
		Delta struct {
			Content   string          `json:"content"`
			ToolCalls []toolCallChunk `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// toolCallChunk 工具调用 delta chunk。
type toolCallChunk struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// parseSSE 解析 OpenAI SSE 流。
func (m *Model) parseSSE(r io.Reader, out chan<- types.Event) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		textSeq    int  // 文本块序号计数器
		curTextIdx = -1 // 当前文本块序号
		toolCalls  = make(map[int]*toolCallAccumulator)
	)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var chunk messageChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		// 文本 delta
		if delta.Content != "" {
			if curTextIdx < 0 {
				curTextIdx = textSeq
				out <- types.Event{Type: types.EventTextStart, Index: curTextIdx}
				textSeq++
			}
			out <- types.Event{Type: types.EventTextDelta, Index: curTextIdx, Text: delta.Content}
		} else if curTextIdx >= 0 {
			out <- types.Event{Type: types.EventTextEnd, Index: curTextIdx}
			curTextIdx = -1
		}

		// 工具调用 delta
		for _, tc := range delta.ToolCalls {
			acc, ok := toolCalls[tc.Index]
			if !ok {
				acc = &toolCallAccumulator{tc: &types.ToolCall{ID: tc.ID, Name: tc.Function.Name}}
				toolCalls[tc.Index] = acc
				out <- types.Event{Type: types.EventToolCallStart, TC: acc.tc, Index: tc.Index}
			}
			if tc.ID != "" {
				acc.tc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				acc.tc.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.tc.Arguments += tc.Function.Arguments
				out <- types.Event{Type: types.EventToolCallDelta, TC: acc.tc, Index: tc.Index}
			}
		}
	}

	if scanner.Err() != nil {
		out <- types.Event{Type: types.EventError, Err: scanner.Err()}
		return
	}

	// 关闭未完成的文本块
	if curTextIdx >= 0 {
		out <- types.Event{Type: types.EventTextEnd, Index: curTextIdx}
	}

	// 关闭所有工具调用块
	for idx, acc := range toolCalls {
		out <- types.Event{Type: types.EventToolCallEnd, TC: acc.tc, Index: idx}
	}

	out <- types.Event{Type: types.EventDone}
}

// ────────────────── 错误工具函数 ──────────────────

// wrapProviderError 包装错误为 llmkit Error。
func wrapProviderError(code types.ErrorCode, provider, message string, cause error) error {
	e := types.NewError(code, provider, message)
	if cause != nil {
		e = e.WithCause(cause)
	}
	return e
}
