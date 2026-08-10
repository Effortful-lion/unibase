# simple_agent

基于 llmkit 构建的通用 Agent 运行时。管理 LLM ↔ 工具的自动循环，支持同步/流式两种调用方式，内置 MCP、Memory、RAG 扩展能力。

## 功能矩阵

| 能力 | 状态 | 说明 |
|---|---|---|
| Agent 循环引擎 | ✅ | LLM → 工具调用 → 执行 → 注入 → 往复 |
| 同步 API | ✅ | Ask / AskWithTools / AskWithConfig |
| 流式 API | ✅ | AskStream / AskStreamWithTools / AskStreamWithConfig |
| 底层 API | ✅ | Agent.Run / Continue / Reset / Messages |
| Tool 接口 | ✅ | Name / Definition / Execute |
| 事件系统 | ✅ | Agent 层 Event + 高层 StreamEvent |
| Session 持久化 | ✅ | JSONL 格式，Save/Load/List/Delete |
| 扩展能力 | 状态 | 说明 |
|---|---|---|
| MCP 桥接 | ✅ | 默认 stdio + JSON-RPC，可替换实现 |
| Memory 记忆 | ✅ | 默认滑动窗口，可实现自定义 Memory 接口 |
| RAG 检索 | ✅ | 默认 TF-IDF，可替换为 embedding + 向量库 |
| 错误体系 | ✅ | Agent 级 ErrorCode |
| 零外部依赖 | ✅ | 仅依赖 llmkit |

## 快速开始

### 1. 同步对话

```go
p := openai.New(&provider.ProviderOptions{APIKey: "sk-..."})

// 最简单：一个问题，一个答案
result, err := simple_agent.Ask(ctx, p, "gpt-4o", "你好")
```

### 2. 带工具的对话

```go
// 定义工具
type weatherTool struct{}
func (w *weatherTool) Name() string                          { return "get_weather" }
func (w *weatherTool) Definition() schema.ToolInfo           { ... }
func (w *weatherTool) Execute(ctx context.Context, args string) (string, error) { ... }

// 调用
result, toolResults, err := simple_agent.AskWithTools(ctx, p, "gpt-4o",
    "北京今天天气", []simple_agent.Tool{&weatherTool{}})
// toolResults[0].Result 包含工具执行结果
```

### 3. 流式对话

```go
stream, _ := simple_agent.AskStream(ctx, p, "gpt-4o", "你好")
for evt := range stream {
    switch evt.Type {
    case simple_agent.StreamEventTextDelta:
        fmt.Print(evt.Text)
    case simple_agent.StreamEventDone:
        fmt.Println("\n[完成]")
    }
}
```

## API 参考

### 同步 API

| 方法 | 用途 | 返回 |
|---|---|---|
| `Ask(ctx, p, model, prompt)` | 最简单对话 | `(string, error)` |
| `AskWithTools(ctx, p, model, prompt, tools)` | 带工具调用 | `(string, []ToolResult, error)` |
| `AskWithConfig(ctx, p, AskConfig)` | 全配置 | `(string, []ToolResult, error)` |

### 流式 API

| 方法 | 用途 | 返回 |
|---|---|---|
| `AskStream(ctx, p, model, prompt)` | 流式对话 | `(<-chan StreamEvent, error)` |
| `AskStreamWithTools(ctx, p, model, prompt, tools)` | 流式 + 工具 | `(<-chan StreamEvent, error)` |
| `AskStreamWithConfig(ctx, p, AskConfig)` | 流式全配置 | `(<-chan StreamEvent, error)` |

### 底层 API

| 方法 | 用途 |
|---|---|
| `New(Config) *Agent` | 创建 Agent |
| `Run(ctx, userInput) <-chan Event` | 启动一轮对话 |
| `Continue(ctx, extraContext) <-chan Event` | 继续对话 |
| `Reset()` | 清空历史 |
| `Messages() []types.Message` | 获取历史 |

## 配置结构

```go
// AskConfig — 全配置同步/流式调用
simple_agent.AskConfig{
    Model        string       // 模型 ID，如 "gpt-4o"
    Prompt       string       // 用户输入
    SystemPrompt string       // 系统提示词
    Tools        []Tool       // 可用工具
    Temperature  float64      // 0 = 模型默认
    MaxTokens    int          // 0 = 不限制
    MaxSteps     int          // 默认 10，工具调用最大循环轮数
}

// Agent.Config — 底层 API 配置
simple_agent.Config{
    Provider     provider.Provider
    Model        string
    SystemPrompt string
    Tools        []Tool
    MaxSteps     int
    Temperature  float64
    MaxTokens    int
}
```

## Tool 接口

```go
type Tool interface {
    Name() string                                    // 工具唯一标识
    Definition() schema.ToolInfo                     // 传递给 LLM 的定义
    Execute(ctx context.Context, args string) (string, error)  // 执行逻辑
}
```

## MCP 集成

```go
// 一行接入 MCP 工具
tools, close, err := mcp.NewClientAndBridge(ctx, mcp.Config{
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/path/to/dir"},
})
defer close()

result, toolResults, err := simple_agent.AskWithTools(ctx, p, "gpt-4o",
    "列出目录下的文件", tools)
```

## Memory 记忆管理

```go
// 创建滑动窗口（保留最近 20 条消息）
mem := memory.NewSlidingWindow(20)

// 每次调用后存入记忆
mem.Inject(agent.Messages())

// 调用前从记忆取回
stored := mem.Retrieve() // 最近 20 条
// 将 stored 合并到调用消息中
```

## RAG 检索增强

```go
// 1. 创建 RAG 引擎
r := rag.New(rag.Config{
    ChunkSize:   500,
    ChunkOverlap: 50,
    TopK:        3,
})

// 2. 添加文档
r.AddDocument("faq", "北京是中国的首都，人口约 2100 万。")
r.AddDocument("faq", "上海是中国的经济中心，人口约 2400 万。")

// 3. 检索
results, _ := r.Retrieve(ctx, "北京有多少人口？")

// 4. 注入 prompt
context := r.BuildContext(results)
answer, _ := simple_agent.Ask(ctx, p, "gpt-4o",
    context + "\n\n问题：北京有多少人口？")
```

## Session 持久化

```go
// 保存
err := agent.SaveSession("my-chat")

// 加载
messages, err := simple_agent.LoadSession("my-chat")

// 列出所有
sessions, _ := simple_agent.ListSessions()
for _, s := range sessions {
    fmt.Printf("%s (%d messages)\n", s.ID, s.MsgCount)
}

// 删除
simple_agent.DeleteSession("my-chat")
```

## 事件类型

### 底层事件（Agent.Run 返回）

```go
EventStepStart   // 一轮 LLM 调用开始
EventStepEnd     // 一轮结束（含 Usage）
EventTextDelta   // 文本增量
EventToolCall    // 工具调用
EventToolResult  // 工具结果
EventDone        // Agent 运行结束
EventError       // 出错
```

### 高层流式事件（AskStream 返回）

```go
StreamEventTextDelta
StreamEventToolCall
StreamEventToolResult
StreamEventDone
StreamEventError
```

## 包结构

```
simple_agent/
├── agent.go          Agent 核心, 高层 API (Ask/AskStream 系列)
├── event.go          Agent 事件系统, StreamEvent, ToolResult
├── tool.go           Tool 接口
├── error.go          Agent 错误类型
├── session.go        Session 持久化 (JSONL)
├── mcp/              MCP 工具桥接
│   ├── client.go     JSON-RPC stdio 客户端
│   └── bridge.go     MCP → simple_agent.Tool 桥接
├── memory/           记忆管理
│   └── sliding.go    滑动窗口记忆
├── rag/              检索增强生成
│   └── retriever.go  TF-IDF 检索引擎
└── example/
    └── main.go       8 个场景演示（mock provider，无需配置）
```

## 运行示例

```bash
cd simple_agent
go run ./example
```

示例使用内置 mock provider，不需要任何 API key 或网络请求。

## 扩展能力（内置默认实现，可替换）

simple_agent 内置了 MCP、Memory、RAG 的默认实现，开箱即用。同时这些模块都是可替换的——实现方可以提供自己的实现，只需满足对应接口即可。

### MCP 工具桥接（`mcp/`）

内置默认实现：stdio + JSON-RPC 2.0 客户端。

```go
// 使用默认实现：一行接入 MCP 工具
tools, close, err := mcp.NewClientAndBridge(ctx, mcp.Config{
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/path/to/dir"},
})
defer close()

result, toolResults, err := simple_agent.AskWithTools(ctx, p, "gpt-4o",
    "列出目录下的文件", tools)
```

扩展方式：实现自定义 `mcp.Client` 接口或自定义 `BridgeTool` 转换逻辑。

### Memory 记忆管理（`memory/`）

内置默认实现：`SlidingWindow` 滑动窗口，保留最近 N 条消息。

```go
mem := memory.NewSlidingWindow(20)
mem.Inject(agent.Messages())
stored := mem.Retrieve()
```

扩展方式：实现 `memory.Memory` 接口（`Inject` / `Retrieve` / `Clear`），可替换为摘要记忆、向量记忆等。

### RAG 检索增强（`rag/`）

内置默认实现：TF-IDF 检索引擎，零依赖，自动分块。

```go
r := rag.New(rag.Config{ChunkSize: 500, TopK: 3})
r.AddDocument("doc1", "北京是中国的首都...")
results, _ := r.Retrieve(ctx, "北京有多少人口？")
```

扩展方式：替换 `Retrieve()` 方法接入 embedding 服务、向量数据库等。

## 设计原则

- 一个需求一个方法，调用方不需要理解内部循环
- 同步/流式 API 分离，各取所需
- 底层 API 保留，需要完全控制时降级使用
- 零外部框架依赖
- 扩展能力（MCP / Memory / RAG）提供默认实现，可直接使用，也可替换为自定义实现
