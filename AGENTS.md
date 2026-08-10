# unibase AGENTS

本文件面向在 unibase 项目中工作的 AI Agent，定义项目设计哲学、代码组织方式和编码约定。
阅读本文档后，Agent 应能理解项目的核心原则并产出风格一致的代码。

---

## 1. 项目是什么

unibase 是一套 Go 个人后端开发底座工具箱，采用单仓库多 Module（Monorepo）模式，支持渐进式迭代扩展。
遵循 MIT 开源协议。

**定位**：沉淀个人项目反复复用的基础设施能力，只做封装适配，不重复造轮子，不实现完整业务系统。
私有仓库起步，未来可直接对外开源。

---

## 2. 设计哲学

所有代码遵循以下原则：

- **简单优先**：功能以基础够用为目标，不追求大而全。
- **入口单一**：每个模块对外只暴露一个清晰的入口（通常是一个 `New` 函数 + 包级别便捷函数），使用者不需要理解内部实现即可上手。
- **不过度封装**：避免不必要的抽象层、工厂模式、策略模式等复杂结构。能用直接的结构体+方法解决的问题，就不用接口+注册表。
- **不过度设计**：先解决当前问题，再考虑扩展。没有实际需求的 "未来兼容" 都是过度设计。
- **扁平化结构**：模块内部采用扁平化文件结构，同层级的 `.go` 文件按职责拆分，不建立深层嵌套的子包。一个模块的所有代码直接位于模块根目录的包中。

---

## 3. 模块结构

### 3.1 现有模块

```
unibase/
├── logx/       # 结构化日志（已发布）
├── httpx/      # HTTP 客户端/服务端
├── rpcx/       # RPC 相关
├── adapter/    # 适配层（原 adapter-kit）
├── component/  # 通用组件（状态机等）
├── plugins/    # 插件
├── tools/      # 工具函数
├── llmkit/     # LLM 开发库（多包结构，见 3.3）
├── configx/    # 配置管理
├── docs/       # 文档
└── README.md   # 项目总览
```

### 3.2 模块依赖规则

依赖方向**单向向下**，不允许反向依赖，不允许循环依赖。

```
llmkit
  ├── logx         （日志）
  └── 标准库 / 外部 HTTP SDK

adapter
  ├── baseutil     （如果存在）
  └── 标准库

httpx / rpcx / component / plugins / tools / configx
  └── 标准库（当前阶段不互相依赖）
```

**规则**：
- 新增模块时只能依赖已存在的下层模块或标准库
- 不允许现有模块反向依赖新模块（除非新模块在依赖链下方）
- 在 `go.work` 中注册新模块
- 不允许在模块内引入公司内部依赖
- 基础工具模块（如 `logx`）不依赖业务相关模块

### 3.3 llmkit 模块特殊规则

`llmkit` 是 LLM 开发库，因类型体系复杂，采用**多包结构**（例外于扁平化原则）：

```
llmkit/
├── go.mod
├── types/                    # 核心类型：Message, Role, ToolCall, ContentBlock, Options
├── schema/                   # 扩展类型：ToolInfo, ToolChoice
└── provider/                 # Provider 实现层
    ├── provider.go           # Provider 接口 + Register/Get/NewProvider 工厂
    ├── options.go            # ProviderOptions 统一配置
    ├── openai/               # OpenAI 兼容实现
    └── anthropic/            # Anthropic Claude 实现
```

**llmkit 设计原则**：
- `types/` 和 `schema/` 是无依赖的核心类型层，只依赖标准库
- `provider/` 实现层依赖 `types/` 和 `schema/`，以及外部 HTTP SDK
- 用户按需 import 子包，不提供 Facade 模式
- Provider 之间不互相依赖

---

## 4. 编码约定

### 4.1 包名

- 包名使用短小、有意义的名词，全小写，无下划线
- 例如：`logx`、`httpx`、`rpcx`、`llmkit`、`types`、`provider`

### 4.2 文件命名

- 文件名使用小写蛇形命名：`logger.go`、`writer.go`、`entry.go`
- 每个文件专注单一职责，不要把所有代码塞进一个文件
- `doc.go` 专门存放包级文档注释（每个子包都应该有）

### 4.3 类型与接口

- 优先使用具体类型（结构体），仅当确实需要多态时才定义接口
- 接口只定义必要的方法，保持最小化（3-5 个方法为佳）
- 结构体字段使用小写导出（私有），通过方法暴露能力
- 方法链式调用时返回 `*T`，方便流畅使用

### 4.4 导出规则

- 对外 API 使用 PascalCase 导出
- 内部实现使用 camelCase 保持私有
- 一个类型或函数的职责应该单一，命名反映其实际行为

### 4.5 包级别便捷函数

- 模块可以提供包级别的便捷函数，降低使用成本
- 便捷函数内部委托给默认实例，不要在便捷函数中添加额外逻辑
- 示例：`logx.Info(msg, fields)` 委托给 `defaultLogger.Info(msg, fields)`

### 4.6 默认实例

- 提供一个合理的默认实例，让用户零配置即可使用
- 提供 `New(...)` 函数让用户自定义创建实例
- 提供 `SetDefault()` / `Default()` 函数支持替换默认实例

### 4.7 测试

- 测试文件与被测文件同目录，命名与被测文件对应
- 测试函数命名清晰：`TestXxx_Scenario`
- 覆盖正常路径、边界条件和错误路径
- 不追求 100% 覆盖率，但核心逻辑必须有测试

### 4.8 错误处理

- 定义领域特定的 `ErrorCode` 常量，配合 `errors.Is` 做错误判断
- 错误消息使用小写开头，不携带模块前缀（模块前缀由 log 框架处理）
- 提供便捷判断函数：`IsRateLimitError(err)`、`IsTimeoutError(err)` 等
- 双层错误体系：
  - 函数级错误：`return error`（无法通信的系统错误）
  - 响应级错误：`Response.Error`（API 返回的业务错误）

### 4.9 并发安全

- 共享可变状态必须使用 `sync.Mutex` 保护
- 不依赖调用方的顺序保证

### 4.10 命名规范

所有命名必须遵循以下规则：

- **遵循 golangci-lint 规范**：命名必须通过 `golangci-lint` 检查
- **全含义命名**：禁止使用缩写。每个词必须完整写出，不能省略字母
  - `file` 不要命名为 `f`
  - `fileName` 不要命名为 `fn`
  - `userInfo` 不要命名为 `ui`
  - `config` 不要命名为 `cfg` 或 `conf`
- **驼峰映射**：第一个单词首字母小写，后续每个独立单词首字母大写
  - 私有变量/函数：`userName`、`filePath`、`requestID`
  - 导出变量/函数：`UserName`、`FilePath`、`RequestID`
  - 常量：`MaxRetryCount`、`DefaultTimeout`
- **名词优先**：类型名使用名词或名词短语（`UserService`、`FileWriter`），函数名使用动词开头（`GetUser`、`WriteFile`）
- **布尔值命名**：使用 `is`、`has`、`can`、`should` 等前缀，如 `isEnabled`、`hasError`、`canRetry`

### 4.11 导出规则

- 只有对外公开的 API 才用大写开头（导出）
- 包内部辅助函数用小写开头（不导出）
- 判断标准：这个函数是"用户需要直接调用的"还是"包内部用的"
- 反例：`FromFile`/`FromBytes`/`MustLoad` 只是 `ReadConfig` 的内部辅助，应该小写
- 正确做法：包级公开 API 保持大写（`New`/`ReadConfig`/`SearchUpward`），内部函数小写

### 4.12 小函数不要拆

- 如果一个函数体只有一两行、逻辑一目了然，不要拆成多个函数
- 不要为了"复用"而拆分：只有被**多个地方**真正调用的逻辑才值得独立成函数
- 反例：`SearchUpwardWithExt` 只是 `SearchUpward` 加了个前缀拼接，完全没必要拆成两个函数
- 好的做法：把相关逻辑写在一个函数里，保持阅读流畅性

---

## 5. 配置与选项模式

### 5.1 函数式选项

- 使用 `Option func(*T)` 模式提供可选的配置项
- 选项函数命名以 `With` 开头：`WithModel()`、`WithTemperature()`
- 配置结构体的可选字段使用**指针类型**，nil 表示"未设置，使用默认值"
  - `Temperature *float32` 而非 `Temperature float32`
  - 这样可以将"用户显式设置为 0"和"用户未设置"区分开

### 5.2 配置结构体

- 配置结构体命名为 `XxxOptions` 或 `XxxConfig`
- 必需字段直接作为结构体字段（非指针）
- 可选字段使用指针类型
- 提供 `NewXxx(opts ...Option) *Xxx` 构造函数

---

## 6. Provider 设计规范（llmkit）

### 6.1 Provider 接口

```go
type Provider interface {
    Name() string
    Chat(ctx context.Context, opts *ProviderOptions, messages []Message) (*Response, error)
    ChatStream(ctx context.Context, opts *ProviderOptions, messages []types.Message) (<-chan types.StreamChunk, error)
}
```

### 6.2 Provider 注册

- 使用注册表模式：`provider.Register(name, factory)` + `provider.Get(name)` + `provider.NewProvider(name, opts...)`
- 各 provider 子包在 `init()` 中自动注册
- 注册表使用 `sync.RWMutex` 保护并发安全

### 6.3 消息类型

- 使用 `types.Message` 作为统一消息格式
- Role 使用常量：`RoleSystem`、`RoleUser`、`RoleAssistant`、`RoleTool`
- 多模态通过 `ContentBlock` 支持：`BlockText`、`BlockThinking`、`BlockImage`、`BlockAudio`、`BlockVideo`
- Provider 特定字段通过 `Message.Extra map[string]any` 透传，不修改核心类型

### 6.4 流式响应

- 使用 `<-chan StreamChunk` 作为流式返回值（channel 模式）
- `StreamChunk` 包含 `Content`、`ToolCalls`、`Usage`、`Done`、`Err`
- 流结束时关闭 channel，`Done = true` 表示最后一块

### 6.5 错误处理

- 使用 `types.ErrorCode` 标准化错误码
- 支持 `errors.Is` 判断错误类型
- 提供便捷函数：`IsRateLimitError(err)`、`IsTimeoutError(err)`、`IsAuthenticationError(err)`

---

## 7. 参考实现

### 7.1 logx 模块

`logx` 是 unibase 中第一个模块，体现了上述所有设计原则：

| 文件 | 职责 | 说明 |
|------|------|------|
| `doc.go` | 包文档 | 核心设计说明 + 快速开始示例 |
| `level.go` | 日志级别 | Level 类型、常量、String/Pars |
| `entry.go` | 日志条目 | Entry 结构体、Fields 类型、格式化 |
| `writer.go` | 输出接口与实现 | Writer/Formatter 接口、ConsoleWriter、FileWriter、MultiWriter |
| `logger.go` | 日志器 | Logger 结构体、New/Module/With 构造、包级别便捷函数 |
| `logger_test.go` | 测试 | 覆盖 Logger、Writer、Formatter 功能 |
| `logname_test.go` | 测试 | 覆盖 LogNamePattern 功能 |

### 7.2 llmkit 模块

`llmkit` 是 LLM 开发库，作为多包结构的参考：

| 包 | 职责 | 说明 |
|----|------|------|
| `types/` | 核心类型 | Message、Role、ToolCall、ContentBlock、CallOptions、ErrorCode、StreamChunk |
| `schema/` | 扩展类型 | ToolInfo、ToolChoice |
| `provider/` | Provider 层 | Provider 接口、注册表、各 provider 实现 |

### 7.3 configx 模块

`configx` 是配置管理模块，参考 `logx` 的扁平化结构：

| 文件 | 职责 | 说明 |
|------|------|------|
| `doc.go` | 包文档 | 核心设计说明 + 快速开始示例 |
| `config.go` | 配置核心 | Config 结构体、加载逻辑 |
| `source.go` | 配置源 | 配置来源抽象（文件、环境变量、远程） |
| `reader.go` | 配置读取 | 读取器接口与实现 |
| `README.md` | 文档 | 用途、安装、最小例子 |

---

## 8. 扩展新模块的流程

1. 在项目根目录创建模块目录，如 `newmod/`
2. 在模块内创建 `doc.go`，描述用途和核心设计
3. 按职责拆分 `.go` 文件，保持扁平化（llmkit 等特殊模块除外）
4. 实现核心功能，确保有对应测试
5. 提供 `New` 构造函数和包级别便捷函数
6. 确保模块独立，不依赖其他模块（除非明确声明）
7. 在 `go.work` 中注册新模块
8. 在本文档的模块列表中补充说明

---

## 9. 禁止事项

- 不要在通用模块内建立子包（如 `logx/internal/`、`logx/formatter/`）
- 不要为了 "通用性" 添加没有实际使用场景的接口和扩展点
- 不要使用反射、代码生成等复杂手段解决简单问题
- 不要在包级别保存可变全局状态（除了明确的默认实例）
- 不要引入外部日志、配置、网络框架等重型依赖（除非模块定位需要）
- 不允许在模块内引入公司内部依赖（ims、oin/redlock 等）
- 不使用 Facade 模式（如 `llmlib.go` 统一导出所有类型），让用户直接 import 子包
