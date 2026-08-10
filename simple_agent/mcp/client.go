// Package mcp 提供了 MCP (Model Context Protocol) 工具桥接。
//
// MCP 是一种标准协议，让 LLM 能够调用外部工具。
// 本包实现了 MCP 客户端，通过 stdio 与 MCP server 通信，
// 并将 MCP 工具桥接为 simple_agent.Tool，使其可以被 Agent 调用。
//
// 使用示例：
//
//	// 1. 创建 MCP 客户端
//	client, err := mcp.NewClient(ctx, "node", []string{"./mcp-server.js"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	// 2. 列出可用工具
//	tools, err := client.ListTools(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("可用工具: %d\n", len(tools))
//
//	// 3. 将 MCP 工具桥接为 simple_agent.Tool
//	var agentTools []simple_agent.Tool
//	for _, tool := range tools {
//	    agentTools = append(agentTools, mcp.BridgeTool(client, tool))
//	}
//
//	// 4. 在 Agent 中使用
//	result, toolResults, err := simple_agent.AskWithTools(ctx, p, "gpt-4o",
//	    "查询北京天气", agentTools)
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Client 是 MCP 客户端，通过 stdio 与 MCP server 通信。
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
	id     int
}

// Config 配置 MCP 客户端。
type Config struct {
	// Command 是 MCP server 的命令，如 "node", "python", "npx"
	Command string
	// Args 是命令参数，如 ["./server.js"] 或 ["-y", "@modelcontextprotocol/server-filesystem", "/path"]
	Args []string
}

// NewClient 创建 MCP 客户端并启动 server 进程。
//
// ctx 用于控制 server 进程的生命周期，ctx 取消时 server 进程会被终止。
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Command == "" {
		return nil, errors.New("mcp: command is required")
	}

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start server: %w", err)
	}

	client := &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
	}

	// 发送 initialize 请求
	if err := client.initialize(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("mcp: initialize: %w", err)
	}

	return client, nil
}

// Close 关闭客户端并终止 server 进程。
func (c *Client) Close() error {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	return nil
}

// ────────────────── JSON-RPC 通信 ──────────────────

// rpcRequest JSON-RPC 2.0 请求。
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse JSON-RPC 2.0 响应。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError JSON-RPC 2.0 错误。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcNotification JSON-RPC 2.0 通知（无 ID）。
type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

// send 发送 JSON-RPC 请求并等待响应。
func (c *Client) send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.id++
	id := c.id

	var paramsBytes json.RawMessage
	if params != nil {
		var err error
		paramsBytes, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
	}

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsBytes,
	}

	if err := json.NewEncoder(c.stdin).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	decoder := json.NewDecoder(c.stdout)
	for {
		var resp rpcResponse
		if err := decoder.Decode(&resp); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}

		// 忽略通知（无 ID）
		if resp.ID == 0 {
			continue
		}

		if resp.ID != id {
			continue // 跳过不匹配的响应
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		return resp.Result, nil
	}
}

// ────────────────── MCP 协议 ──────────────────

// initialize 发送 initialize 请求。
func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "simple_agent",
			"version": "1.0.0",
		},
	}

	result, err := c.send(ctx, "initialize", params)
	if err != nil {
		return err
	}

	// 发送 initialized 通知
	notif := rpcNotification{JSONRPC: "2.0", Method: "notifications/initialized"}
	if err := json.NewEncoder(c.stdin).Encode(notif); err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}

	_ = result // 可以解析 server capabilities
	return nil
}

// ────────────────── MCP 类型 ──────────────────

// Tool MCP 工具定义。
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ListToolsResult tools/list 响应结果。
type listToolsResult struct {
	Tools []Tool `json:"tools"`
}

// ToolCallResult tools/call 响应结果。
type toolCallResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ────────────────── 公开 API ──────────────────

// ListTools 列出 server 提供的所有工具。
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	result, err := c.send(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var listResult listToolsResult
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("parse tools list: %w", err)
	}

	return listResult.Tools, nil
}

// CallTool 调用指定工具。
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	params := map[string]any{
		"name":      name,
		"arguments": arguments,
	}

	result, err := c.send(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}

	var callResult toolCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		return "", fmt.Errorf("parse tool result: %w", err)
	}

	if callResult.IsError {
		return "", errors.New("mcp: tool execution failed")
	}

	// 拼接所有 text 内容
	var resultText string
	for _, item := range callResult.Content {
		if item.Type == "text" {
			resultText += item.Text
		}
	}

	return resultText, nil
}
