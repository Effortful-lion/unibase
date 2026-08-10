# llmkit

LLM 开发基础库，提供类型定义、Provider 抽象、注册机制和真实 Provider 实现。

零外部依赖，仅使用 Go 标准库。

## 功能矩阵

| 能力 | 状态 | 说明 |
|---|---|---|
| 类型系统 | ✅ | Message, Role, ContentBlock, ToolCall, Usage |
| 流式事件 | ✅ | EventType + Event + Stream（channel + close 模式） |
| 错误体系 | ✅ | ErrorCode + Error，支持 errors.Is |
| 工具定义 | ✅ | ToolInfo, ToolCall, ToolChoice |
| Provider 接口 | ✅ | Provider + Model 接口，注册机制 |
| 调用配置 | ✅ | ProviderOptions（创建）+ ChatOptions + CallOption（调用） |
| OpenAI Provider | ✅ | 真实 HTTP 调用，SSE 流式解析 |
| Anthropic Provider | ✅ | 真实 HTTP 调用，SSE 流式解析 |

## 核心类型

### 消息

```go
types.Message{Role, Content, Parts, ToolCallID, ToolName}
types.RoleSystem / RoleUser / RoleAssistant / RoleTool
types.ContentBlock{Type, Text, Thinking, ToolCall, ImageURL}
```

### 流式事件

```go
types.Event{Type, Text, Index, TC, Usage, Err}
types.Stream <-chan Event

// 事件类型
types.EventStart / EventTextDelta / EventTextStart / EventTextEnd
types.EventToolCallStart / EventToolCallDelta / EventToolCallEnd
types.EventThinkingDelta
types.EventDone / EventError
```

### 错误

```go
types.Error{Code, Message, Cause, Details}
types.ErrCodeUnknown / ErrCodeAuthentication / ErrCodeRateLimit
types.ErrCodeTimeout / ErrCodeCanceled / ErrCodeTokenLimit / ...
```

### 工具定义

```go
schema.ToolInfo{Name, Description, Parameters, Strict}
schema.ToolCall{ID, Name, Arguments}
schema.ToolChoiceNone / ToolChoiceAuto / ToolChoiceRequired
```

## Provider 接口

```go
provider.Provider{
    Name() string
    Models() []ModelInfo
    Model(modelID string) (Model, error)
    Chat(ctx, messages, opts) (*Response, error)
    ChatStream(ctx, messages, opts) (<-chan types.Event, error)
}

provider.Model{
    Info() ModelInfo
    Chat(ctx, messages, opts) (*Response, error)
    ChatStream(ctx, messages, opts) (<-chan types.Event, error)
}
```

## 用法

### 创建 Provider

```go
// OpenAI
import "github.com/Effortful-lion/unibase/llmkit/provider/openai"
p := openai.New(&provider.ProviderOptions{
    APIKey:  "sk-...",
    BaseURL: "https://api.openai.com/v1", // 可选，默认值
})

// Anthropic
import "github.com/Effortful-lion/unibase/llmkit/provider/anthropic"
p := anthropic.New(&provider.ProviderOptions{
    APIKey:  "sk-ant-...",
    BaseURL: "https://api.anthropic.com/v1",
})
```

### 调用（同步）

```go
response, err := p.Chat(ctx, []types.Message{
    {Role: types.RoleUser, Content: "你好"},
}, &provider.ChatOptions{
    Model:       provider.WithModel("gpt-4o"),
    Temperature: provider.WithTemperature(0.7),
    MaxTokens:   provider.WithMaxTokens(1024),
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(response.Choices[0].Message.Content)
```

### 调用（流式）

```go
stream, err := p.ChatStream(ctx, messages, &provider.ChatOptions{
    Model: provider.WithModel("gpt-4o"),
})
if err != nil {
    log.Fatal(err)
}

for evt := range stream {
    switch evt.Type {
    case types.EventTextDelta:
        fmt.Print(evt.Text)
    case types.EventDone:
        fmt.Printf("\n[完成] tokens: %d\n", evt.Usage.TotalTokens)
    case types.EventError:
        log.Fatal(evt.Err)
    }
}
```

### 使用工具

```go
tools := []schema.ToolInfo{{
    Name:        "get_weather",
    Description: "查询城市天气",
    Parameters: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "city": map[string]any{
                "type":        "string",
                "description": "城市名称",
            },
        },
        "required": []string{"city"},
    },
}}

stream, _ := p.ChatStream(ctx, messages, &provider.ChatOptions{
    Model:  provider.WithModel("gpt-4o"),
    Tools:  provider.WithTools(tools),
})

for evt := range stream {
    switch evt.Type {
    case types.EventToolCallStart:
        fmt.Printf("调用工具: %s\n", evt.TC.Name)
    case types.EventToolCallEnd:
        fmt.Printf("参数: %s\n", evt.TC.Arguments)
    }
}
```

### Provider 注册机制

```go
// 实现 Provider 接口
type myProvider struct { ... }

func init() {
    provider.Register("my-provider", func(opts *provider.ProviderOptions) (provider.Provider, error) {
        return NewMyProvider(opts)
    })
}

// 按名称创建
p, err := provider.NewProvider("my-provider",
    provider.WithAPIKey("key"),
    provider.WithBaseURL("http://localhost:8080"),
)
```

## 包结构

```
llmkit/
├── types/
│   ├── message.go    Message, Role, ContentBlock, ToolCall
│   ├── event.go      EventType, Event, Stream
│   ├── error.go      ErrorCode, Error
│   └── usage.go      Usage
├── schema/
│   └── tool.go       ToolInfo, ToolCall, ToolChoice, ToolResult
└── provider/
    ├── provider.go   Provider 接口, Model 接口, ModelInfo, 注册表
    ├── options.go    ProviderOptions, ChatOptions, CallOption
    ├── openai/       OpenAI 真实实现
    └── anthropic/    Anthropic 真实实现
```

## 设计原则

- 只定义接口和类型，不包含 Agent 运行时逻辑
- 零外部依赖
- 流式传输使用 channel + close 模式，符合 Go 惯用法
- Provider 注册机制支持按名称创建实例
- 真实 Provider 实现遵循各厂商 API 规范
- 实现方可通过 `provider.Register()` 注册自定义 Provider，或直接实现 `Provider` 接口
