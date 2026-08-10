// Package mcp 提供了 MCP (Model Context Protocol) 工具桥接。
//
// 本文件提供将 MCP 工具转换为 simple_agent.Tool 的桥接功能。
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Effortful-lion/unibase/llmkit/schema"
	"github.com/Effortful-lion/unibase/simple_agent"
)

// BridgeTool 将 MCP 工具桥接为 simple_agent.Tool。
//
// 返回的 Tool 实现了 simple_agent.Tool 接口，
// 其 Definition() 返回 MCP 工具的定义，Execute() 调用 MCP server 执行工具。
//
// 使用示例：
//
//	client, _ := mcp.NewClient(ctx, mcp.Config{Command: "node", Args: []string{"./server.js"}})
//	defer client.Close()
//
//	tools, _ := client.ListTools(ctx)
//	for _, tool := range tools {
//	    agentTools = append(agentTools, mcp.BridgeTool(client, tool))
//	}
func BridgeTool(client *Client, mcpTool Tool) simple_agent.Tool {
	return &mcpToolBridge{
		client: client,
		tool:   mcpTool,
	}
}

// mcpToolBridge 将 MCP 工具桥接为 simple_agent.Tool。
type mcpToolBridge struct {
	client *Client
	tool   Tool
}

// Name 返回工具名称。
func (b *mcpToolBridge) Name() string {
	return b.tool.Name
}

// Definition 返回工具定义。
func (b *mcpToolBridge) Definition() schema.ToolInfo {
	return schema.ToolInfo{
		Name:        b.tool.Name,
		Description: b.tool.Description,
		Parameters:  b.tool.InputSchema,
	}
}

// Execute 执行工具调用。
func (b *mcpToolBridge) Execute(ctx context.Context, argsStr string) (string, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return "", fmt.Errorf("parse tool arguments: %w", err)
	}

	result, err := b.client.CallTool(ctx, b.tool.Name, args)
	if err != nil {
		return "", fmt.Errorf("mcp tool %s: %w", b.tool.Name, err)
	}

	return result, nil
}

// ────────────────── 便捷函数 ──────────────────

// NewClientAndBridge 创建 MCP 客户端并将所有工具桥接为 simple_agent.Tool。
//
// 这是最便捷的使用方式，一行代码完成客户端创建和工具桥接。
//
// 使用示例：
//
//	tools, close, err := mcp.NewClientAndBridge(ctx, mcp.Config{Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/path"}})
//	defer close()
//	result, toolResults, err := simple_agent.AskWithTools(ctx, p, "gpt-4o", "列出目录文件", tools)
func NewClientAndBridge(ctx context.Context, cfg Config) ([]simple_agent.Tool, func() error, error) {
	client, err := NewClient(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create mcp client: %w", err)
	}

	mcpTools, err := client.ListTools(ctx)
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("list mcp tools: %w", err)
	}

	var agentTools []simple_agent.Tool
	for _, tool := range mcpTools {
		agentTools = append(agentTools, BridgeTool(client, tool))
	}

	return agentTools, client.Close, nil
}

// ErrNoTools 表示 MCP server 没有提供任何工具。
var ErrNoTools = errors.New("mcp: server provides no tools")
