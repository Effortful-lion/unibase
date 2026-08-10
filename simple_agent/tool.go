// Package simple_agent 提供了基于 llmkit 的通用 Agent 运行时。
package simple_agent

import (
	"context"

	"github.com/Effortful-lion/unibase/llmkit/schema"
)

// Tool 是 Agent 可调用的工具。
//
// 每个 Tool 实现提供：
//   - Name：唯一标识，LLM 在 tool_call 中使用此名称
//   - Definition：传递给 LLM 的元信息
//   - Execute：运行时执行，参数为 JSON 字符串，返回结果文本
//
// 典型实现是在 Name 和 Definition 返回固定值，在 Execute 中执行实际逻辑。
type Tool interface {
	Name() string
	Definition() schema.ToolInfo
	Execute(ctx context.Context, args string) (string, error)
}
