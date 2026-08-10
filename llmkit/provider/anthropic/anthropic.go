// Package anthropic 实现了 Anthropic Claude Messages API 的 Provider。
package anthropic

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

const (
	defaultBaseURL   = "https://api.anthropic.com/v1"
	anthropicVersion = "2023-06-01"
)

// Config Anthropic Provider 配置。
type Config struct {
	APIKey  string
	BaseURL string
}

func init() {
	provider.Register("anthropic", func(opts *provider.ProviderOptions) (provider.Provider, error) {
		return New(opts)
	})
}

// Provider 是 Anthropic Claude 的 LLM Provider。
type Provider struct {
	cfg    Config
	models []provider.ModelInfo
}

// Model 是 Anthropic 模型的实现。
type Model struct {
	p    *Provider
	info provider.ModelInfo
}

var knownModels = []provider.ModelInfo{
	{ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", ProviderID: "anthropic", ContextWindow: 200000, MaxOutput: 8192},
	{ID: "claude-opus-4", Name: "Claude Opus 4", ProviderID: "anthropic", ContextWindow: 200000, MaxOutput: 16384},
	{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5", ProviderID: "anthropic", ContextWindow: 200000, MaxOutput: 8192},
}

// New 创建 Anthropic Provider。
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
func (p *Provider) Name() string { return "anthropic" }

// Models 返回所有可用模型。
func (p *Provider) Models() []provider.ModelInfo { return p.models }

// Model 获取指定模型。
func (p *Provider) Model(modelID string) (provider.Model, error) {
	for _, m := range p.models {
		if m.ID == modelID {
			return &Model{p: p, info: m}, nil
		}
	}
	return nil, types.NewError(types.ErrCodeInvalidRequest, "anthropic", "unknown model: "+modelID)
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
	return nil, types.NewError(types.ErrCodeInvalidRequest, "anthropic", "model not specified")
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

	url := m.p.cfg.BaseURL + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		out <- types.Event{Type: types.EventError, Err: fmt.Errorf("create request: %w", err)}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-api-key", m.p.cfg.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

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

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
}

type anthropicToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicChatRequest struct {
	Model       string                 `json:"model"`
	MaxTokens   int                    `json:"max_tokens"`
	Stream      bool                   `json:"stream"`
	System      string                 `json:"system,omitempty"`
	Messages    []anthropicChatMessage `json:"messages"`
	Tools       []anthropicToolDef     `json:"tools,omitempty"`
	Temperature float64                `json:"temperature,omitempty"`
}

type anthropicChatMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

// buildRequest 构造 Anthropic Messages API 请求体。
func (m *Model) buildRequest(messages []types.Message, opts *provider.ChatOptions, stream bool) (anthropicChatRequest, error) {
	maxTokens := 4096
	if opts != nil && opts.MaxTokens != nil && *opts.MaxTokens > 0 {
		maxTokens = *opts.MaxTokens
	}

	req := anthropicChatRequest{
		Model:     m.info.ID,
		MaxTokens: maxTokens,
		Stream:    stream,
	}

	// 收集 system prompt
	var sysBuilder strings.Builder
	for _, msg := range messages {
		if msg.Role == types.RoleSystem && msg.Content != "" {
			if sysBuilder.Len() > 0 {
				sysBuilder.WriteByte('\n')
			}
			sysBuilder.WriteString(msg.Content)
		}
	}
	if sysBuilder.Len() > 0 {
		req.System = sysBuilder.String()
	}

	// messages: Anthropic 只支持 user/assistant
	msgs := make([]anthropicChatMessage, 0)
	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			// 已合并到顶层 system
		case types.RoleUser:
			msgs = append(msgs, anthropicChatMessage{
				Role:    "user",
				Content: []anthropicContentBlock{{Type: "text", Text: msg.Content}},
			})
		case types.RoleAssistant:
			blocks := make([]anthropicContentBlock, 0)
			if msg.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: msg.Content})
			}
			for _, b := range msg.Parts {
				if b.Type == types.BlockToolCall && b.ToolCall != nil {
					blocks = append(blocks, anthropicContentBlock{
						Type:  "tool_use",
						ID:    b.ToolCall.ID,
						Name:  b.ToolCall.Name,
						Input: json.RawMessage(b.ToolCall.Arguments),
					})
				}
			}
			msgs = append(msgs, anthropicChatMessage{Role: "assistant", Content: blocks})
		case types.RoleTool:
			msgs = append(msgs, anthropicChatMessage{
				Role: "user",
				Content: []anthropicContentBlock{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Text:      msg.Content,
				}},
			})
		}
	}
	req.Messages = msgs

	// tools
	if opts != nil && len(opts.Tools) > 0 {
		tools := make([]anthropicToolDef, len(opts.Tools))
		for i, t := range opts.Tools {
			tools[i] = anthropicToolDef{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Parameters,
			}
		}
		req.Tools = tools
	}

	if opts != nil && opts.Temperature != nil && *opts.Temperature > 0 {
		req.Temperature = float64(*opts.Temperature)
	}

	return req, nil
}

// ────────────────── 非流式响应解析 ──────────────────

// anthropicNonStreamResponse 非流式 Messages API 响应。
type anthropicNonStreamResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	Model      string                  `json:"model"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
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

	url := m.p.cfg.BaseURL + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", m.p.cfg.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

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

	var body anthropicNonStreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// 构建响应消息
	respMsg := types.Message{Role: types.RoleAssistant}
	for _, block := range body.Content {
		switch block.Type {
		case "text":
			respMsg.Content += block.Text
		case "tool_use":
			respMsg.Parts = append(respMsg.Parts, types.ContentBlock{
				Type: types.BlockToolCall,
				ToolCall: &types.ToolCall{
					ID:        block.ID,
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	return &provider.Response{
		ID:           body.ID,
		Model:        body.Model,
		FinishReason: body.StopReason,
		Usage: &types.Usage{
			PromptTokens:     body.Usage.InputTokens,
			CompletionTokens: body.Usage.OutputTokens,
			TotalTokens:      body.Usage.InputTokens + body.Usage.OutputTokens,
		},
		Choices: []provider.Choice{{
			Message:      respMsg,
			FinishReason: body.StopReason,
		}},
	}, nil
}

// ────────────────── SSE 解析 ──────────────────

// sseContentBlockStart content_block_start 事件。
type sseContentBlockStart struct {
	Index        int `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
}

// sseContentBlockDelta content_block_delta 事件。
type sseContentBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
}

// sseContentBlockStop content_block_stop 事件。
type sseContentBlockStop struct {
	Index int `json:"index"`
}

// sseMessageDelta message_delta 事件。
type sseMessageDelta struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// parseSSE 解析 Anthropic SSE 流。
// 事件格式: "event: <type>" 后跟 "data: <json>"
func (m *Model) parseSSE(r io.Reader, out chan<- types.Event) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		textSeq     int = -1
		toolIdx     int
		textStarted bool
		evtType     string
	)

	type toolState struct {
		id   string
		name string
		args strings.Builder
	}
	tools := make(map[int]*toolState)

	for scanner.Scan() {
		line := scanner.Text()

		// Anthropic SSE: "event: <type>" 后跟 "data: <json>"
		if v, ok := strings.CutPrefix(line, "event: "); ok {
			evtType = strings.TrimSpace(v)
			continue
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}

		switch evtType {
		case "message_start":
			// usage 在 message.usage 中，后续通过 message_delta 获取
		case "content_block_start":
			var cb sseContentBlockStart
			if err := json.Unmarshal([]byte(data), &cb); err != nil {
				continue
			}
			switch cb.ContentBlock.Type {
			case "text":
				textSeq++
				textStarted = true
				out <- types.Event{Type: types.EventTextStart, Index: textSeq}
			case "tool_use":
				toolIdx = cb.Index
				tools[toolIdx] = &toolState{id: cb.ContentBlock.ID, name: cb.ContentBlock.Name}
				out <- types.Event{
					Type:  types.EventToolCallStart,
					Index: toolIdx,
					TC:    &types.ToolCall{ID: cb.ContentBlock.ID, Name: cb.ContentBlock.Name},
				}
			}
		case "content_block_delta":
			var cb sseContentBlockDelta
			if err := json.Unmarshal([]byte(data), &cb); err != nil {
				continue
			}
			switch cb.Delta.Type {
			case "text_delta":
				out <- types.Event{Type: types.EventTextDelta, Index: textSeq, Text: cb.Delta.Text}
			case "input_json_delta":
				if ts, ok := tools[cb.Index]; ok {
					ts.args.WriteString(cb.Delta.PartialJSON)
					out <- types.Event{
						Type:  types.EventToolCallDelta,
						Index: cb.Index,
						TC:    &types.ToolCall{ID: ts.id, Name: ts.name, Arguments: ts.args.String()},
					}
				}
			}
		case "content_block_stop":
			var cb sseContentBlockStop
			if err := json.Unmarshal([]byte(data), &cb); err != nil {
				continue
			}
			if ts, ok := tools[cb.Index]; ok {
				out <- types.Event{
					Type:  types.EventToolCallEnd,
					Index: cb.Index,
					TC:    &types.ToolCall{ID: ts.id, Name: ts.name, Arguments: ts.args.String()},
				}
				delete(tools, cb.Index)
			}
			if cb.Index == textSeq && textStarted {
				out <- types.Event{Type: types.EventTextEnd, Index: textSeq}
				textStarted = false
			}
		case "message_delta":
			var md sseMessageDelta
			if err := json.Unmarshal([]byte(data), &md); err != nil {
				continue
			}
			out <- types.Event{
				Type:  types.EventDone,
				Usage: &types.Usage{CompletionTokens: md.Usage.OutputTokens},
			}
		case "message_stop":
			// 最终结束
		case "ping":
			// 心跳，忽略
		}
	}

	if scanner.Err() != nil {
		out <- types.Event{Type: types.EventError, Err: scanner.Err()}
	}
}
