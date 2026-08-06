# Lambda-Plugins 插件服务架构分析

> 本文档剥离业务特定逻辑，提取插件服务的通用架构设计，可作为从零设计可扩展插件服务的参考。

---

## 1. 整体架构概览

```
┌─────────────────────────────────────────────────────┐
│                  调用方 (Gateway/业务服务)              │
│   请求: x-cos-process=plugin_name/k/v/k/v           │
└──────────────────────┬──────────────────────────────┘
                       │ HTTP
┌──────────────────────▼──────────────────────────────┐
│              LambdaServer (路由/调度层)                 │
│  - 解析表达式: 插件名 + key/value 参数                   │
│  - 服务发现: 从 Redis 选最优插件节点                     │
│  - 负载均衡: 一致性哈希 / 权重评分 / 复用上次节点        │
│  - 协议转换: HTTP 请求 → IMJ 内部协议                    │
└──────────────────────┬──────────────────────────────┘
                       │ IMJ Protocol
┌──────────────────────▼──────────────────────────────┐
│              PluginServer (插件执行层)                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  │
│  │ Plugin A    │  │ Plugin B    │  │ Plugin C    │  │
│  │ (resize)    │  │ (watermark) │  │ (rotate)    │  │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  │
│         │                │                │          │
│  ┌──────▼────────────────▼────────────────▼──────┐  │
│  │            IPluginProvider[T].Invoke()         │  │
│  │   泛型接口: 输入结构化参数 → 执行处理 → 输出结果   │  │
│  └───────────────────────────────────────────────┘  │
│                                                      │
│  并发控制: 每个插件独立 channel 信号量                    │
│  异步任务: PluginTask + Redis 存储 + Webhook 回调       │
└──────────────────────────────────────────────────────┘
```

### 两层架构设计

项目实际上运行着两套框架（来自 `codix` 的不同版本），本项目使用的是 **Lambda 模式**（旧版 `lambda` 包），而 `plugins` 包（新版）作为框架内置能力也存在。

| 层级 | 职责 | 核心类型 |
|------|------|----------|
| **LambdaServer** | 网关路由、服务发现、插件节点选择、协议转换 | `LambdaServer`, `LambdaContext` |
| **PluginServer** | 插件注册、任务生命周期管理、并发控制、IMJ 协议处理 | `Plugin`, `PluginContext`, `PluginTask` |

---

## 2. 核心接口定义

### 2.1 插件提供者接口

```go
// 旧版 Lambda 模式（本项目使用的核心接口）
type ILambdaProvider[T any] interface {
    Invoke(ctx *LambdaContext[T]) error
}

// 新版 Plugins 模式（框架内置）
type IPluginProvider[T any] interface {
    Invoke(ctx *PluginContext[T]) error
}
```

泛型参数 `T` 是每个插件的**参数结构体类型**，框架通过反射自动完成从字符串表达式到结构体的解析。

### 2.2 上下文接口（插件与框架交互的桥梁）

```go
type PluginContext[T any] struct {
    parentCtx  context.Context  // 生命周期上下文
    hs         *ims.HSession    // HTTP 会话（请求/响应）
    Expression *PluginExpression[T] // 解析后的表达式
    pluginTask *PluginTask      // 任务信息
}

// 插件可用的核心方法:
// - ParentCtx()          获取生命周期上下文
// - PluginTask()         获取任务信息 (file_id, task_id, expression)
// - GetExpressionParams() 获取类型化的参数 T
// - GetEcho()            获取 Echo 上下文（写响应）
// - Stream(contentType, code, reader) 流式输出二进制数据
// - Json(code, object)   输出 JSON 响应
// - SSE(handler)         通过 SSE 推送数据
```

### 2.3 表达式与参数解析

```go
type PluginExpression[T any] struct {
    Function      string  // 插件名: "resize"
    RawExpression string  // 原始表达式: "resize/w/640/h/480"
    PluginParams  T       // 解析后的参数: {Width: 640, Height: 480}
}
```

**解析规则**（基于 struct tag）：

```go
type ResizeParams struct {
    Width  int64 `plugin:"w"`    // 匹配 URL 中的 /w/640
    Height int64 `plugin:"h"`    // 匹配 URL 中的 /h/480
    Format string `plugin:"fm"`  // 匹配 URL 中的 /fm/png
}
```

解析流程：
1. 按 `/` 分割表达式 → `["resize", "w", "640", "h", "480"]`
2. 遍历 struct 字段，收集 tag 名到字段的映射
3. 依次消费 key-value 对，通过反射写入对应字段
4. 支持类型：`int/int64`, `float64`, `string`, `bool`

---

## 3. 插件注册与启动流程

### 3.1 注册

```go
ps.WithPlugin[plugins.ResizeParams](
    "resize",                    // 插件名称 → URL 路径
    "1.0",                       // 版本
    "缩略图生成：通过resize/w/60/h/60使用",  // 描述
    plugins.NewResizeLambda(parent, hcli, cosCli),  // 实现实例
    ps.PluginGroup("image"),     // 分组（决定 manual 文档路径）
    ps.WithMaxConcurrent(maxConcurrent), // 并发限制
)
```

`WithPlugin` 内部操作：
1. 创建 `Plugin` 实例，设置默认并发数 = CPU核心数 × 2 + 1
2. 创建 `concurrentLimiter` channel（有缓冲的整数 channel，作为信号量）
3. 注册 HTTP handler → `/v1/cos/lambda/invoke/{name}`
4. 加载 `./doc/{group}.md` 作为插件说明书

### 3.2 启动

```go
ps.StartPlugins(parent,
    ps.WithGroup("photos"),           // 集群分组
    ps.WithSignal(parent),            // 信号监听
    ps.WithClusterClient(clusterClient), // 服务发现
    ps.WithPort(8082),                // 监听端口
    ps.WithPluginListen(httpx.Http{SSL: false}),
    // ... 插件注册
)
```

`StartPlugins` 内部操作：
1. 创建 `ims.Bootstrap` 网关，注册所有插件的 HTTP 路由
2. 启动定期注册：每 5 秒向 Codis 注册中心上报插件状态（endpoint、版本、并发数、CPU/内存指标）
3. 注册 IMJ 协议处理器（PluginTaskInvoke, PluginTaskCancel, PluginTaskInfo, PluginManual, PluginInfo）
4. 监听 SIGTERM 信号，优雅关闭时自动注销

---

## 4. 请求处理流程

### 4.1 外部 HTTP 请求路径（Lambda 模式）

```
客户端
 │  GET /v1/cos/lambda/routes?x-cos-process=resize/w/640/h/480
 │  Headers: Lambda-Upstream-Url: file://..., Lambda-File-Id: fid_xxx
 │
 ▼
LambdaServer.HandleS3LambdaRoutes
 │
 ├─ 1. DetectLambdaMethod → 识别为 "process" 模式
 ├─ 2. ParseExpressionPipes → 解析所有 | 分隔的表达式
 │     ["resize/w/640/h/480", "watermark/t/text/x/20/y/20"]
 │
 ├─ 3. 对每个表达式:
 │   ├─ 3a. selectPluginServer → 选择最优插件节点
 │   │       优先: 复用上次节点 → 一致性哈希 → 权重评分
 │   │
 │   ├─ 3b. firePlugin → 构造内部请求转发
 │   │       POST http://plugin-server:8082/v1/cos/lambda/invoke/resize
 │   │       Body: PluginInvoke { plugin, file_id, expression, task_action }
 │   │
 │   └─ 3c. 将当前响应设为下一个表达式的 upstream
 │
 └─ 4. StreamResponse → 将最终结果流式返回给客户端
```

### 4.2 内部协议路径（PluginServer IMJ 模式）

```
LambdaServer (firePlugin)
 │  POST /v1/cos/lambda/invoke/resize
 │  IMJ Body: { plugin: "resize", file_id: "xxx",
 │             expression: "resize/w/640/h/480",
 │             task_action: "Process" }
 │
 ▼
PluginServer.handler (pluginHandler)
 │
 ├─ 1. 解析 PluginInvoke 请求体
 ├─ 2. 创建 PluginTask { task_id, file_id, expression, action }
 │
 ├─ 3. TaskAction == "Process"（同步）:
 │   ├─ 获取 concurrentLimiter 信号量（最多等 5 秒）
 │   ├─ 创建 turbo.FutureTask（异步执行）
 │   ├─ go actual.Run() 启动
 │   ├─ actual.Get() 等待完成
 │   └─ 插件通过 httpx.StreamResponse 直接写入 HTTP 响应
 │
 ├─ 4. TaskAction == "Submit"（异步）:
 │   ├─ 存储任务到 PluginTaskStorage
 │   ├─ 后台异步执行
 │   └─ 立即返回 PluginInvoke { task_id, status: "Submit" }
 │
 └─ 5. TaskAction == "Cancel":
     └─ 删除任务 + 调用 FutureTask.Cancel()
```

---

## 5. 插件的输入与输出

### 5.1 输入来源

插件不直接接收 HTTP 请求体，而是通过框架提供的上下文获取：

| 输入类型 | 获取方式 | 说明 |
|----------|----------|------|
| **文件标识** | `ctx.PluginTask().GetFileId()` | COS 文件 ID (fid) |
| **结构化参数** | `ctx.GetExpressionParams()` | 从 URL 表达式解析的类型安全参数 |
| **原始表达式** | `ctx.PluginTask().GetExpression()` | 未解析的原始字符串 |
| **任务动作** | `ctx.PluginTask().GetTaskAction()` | Submit / Process / Cancel |
| **生命周期** | `ctx.ParentCtx()` | 用于 cancel 传播的 context |

### 5.2 输出方式

插件通过 `PluginContext` 提供的方法返回结果：

| 输出类型 | 方法 | 适用场景 |
|----------|------|----------|
| **二进制流** | `ctx.Stream(contentType, code, reader, opts...)` | 图片、视频、音频等文件 |
| **JSON** | `ctx.Json(code, obj, opts...)` | 元数据、状态信息 |
| **SSE** | `ctx.SSE(handler, opts...)` | 实时进度推送 |
| **HTTP 状态** | 直接使用 `ctx.GetEcho()` | 自定义响应头、状态码 |

**文件输出的关键点**：
- 插件将处理结果写入临时文件（`/tmp/plugins/{uuid}.ext`）
- 框架通过 `httpx.StreamResponse` 将文件流式传输给客户端
- Content-Type 由插件自己设置，Content-Length 框架自动计算

---

## 6. 异步任务模型

### 6.1 任务状态机

```
                  ┌──────────┐
                  │  Submit  │
                  └────┬─────┘
                       │ 触发执行
                       ▼
                  ┌──────────┐
        ┌─────────│ Processing ├─────────┐
        │         └────┬─────┘          │
        │              │                │
        │    成功       │ 失败           │ 取消
        ▼              ▼                ▼
   ┌─────────┐   ┌──────────┐   ┌──────────┐
   │ Success │   │ WaitRetry│   │ Cancelled │
   └─────────┘   └────┬─────┘   └──────────┘
                      │ 重试次数耗尽
                      ▼
                 ┌──────────┐
                 │ Failure  │
                 └──────────┘
```

### 6.2 任务存储与恢复

- **PluginTaskStorage**（进程内 map + Redis）：存储任务状态
- **WorkerManager**（定时轮询）：每 1 秒扫描 Redis 中的 Submit/WaitRetry 任务
- **RedLock**：分布式锁确保同一任务只被一个 Worker 处理
- **Webhook 回调**：任务完成后主动 POST 结果到回调 URL（最多重试 10 次）

### 6.3 并发控制

每个插件独立维护一个 channel 信号量：

```go
concurrentLimiter := make(chan int, maxConcurrent) // 缓冲 channel

// 执行前获取令牌
concurrentLimiter <- 1
defer func() { <-concurrentLimiter }()

// 超时控制: 5 秒获取不到则返回 "concurrent reach the maximum"
select {
case <-time.After(5 * time.Second):
    return Error
case concurrentLimiter <- 1:
    // 开始执行
}
```

---

## 7. 文件获取策略

### 7.1 Origin 文件共享缓存

多个插件处理同一个文件时，避免重复下载：

```go
// AcquireOriginFile 实现引用计数 + 单次下载
// - 第一个调用者: 触发下载，关闭 waitCh
// - 后续调用者: 等待 waitCh，复用已下载文件
// - 所有调用者 ReleaseOriginFile 后，删除本地文件
```

路径生成规则：`/tmp/plugins/{md5_or_fid}{ext}`

### 7.2 COS 交互

| 操作 | 方法 | 说明 |
|------|------|------|
| 查询文件 | `cosCli.QueryFileInfo(ctx, fid)` | 获取文件元信息（md5、ext、size） |
| 下载文件 | `DownloadFileWithFid(ctx, cosCli, fid, path)` | 下载到本地临时文件 |
| 上传结果 | `cosCli.PutFile(ctx, uploadInfo, reader, opts...)` | 上传处理结果 |
| 更新元数据 | `cosCli.UpdateFileInfo(ctx, req)` | 更新文件 meta 信息 |
| 替换文件 | `cosCli.UpdateFileData(ctx, newFid, oldFid)` | 用新文件替换旧文件 |

---

## 8. 外部依赖封装

### 8.1 工具路径初始化

```go
// 优先级: 环境变量 > 配置文件 > 系统 PATH
func InitToolsPath(homePath common.HomePath) {
    // MAGICK_HOME / FFMPEG_HOME / EXIFTOOL_HOME 环境变量
    // → conf.toml 中的 home_path 配置
    // → 系统默认: magick, ffmpeg, exiftool, ffprobe
}
```

### 8.2 CLI 工具封装

所有外部工具通过统一的 `RunCommandWithEnv` 执行：

```go
func RunCommandWithEnv(ctx context.Context, name string, out io.Writer, args ...string) error {
    return runx.ExecCommandWithOpt(ctx, name,
        runx.WithEnv(buildEnvWithSystemPath()...), // 注入系统 PATH
        runx.WithArgs(args...),
        runx.WithOut(out),
        runx.WithErrOut(out))
}
```

| 工具 | 封装函数 | 功能 |
|------|----------|------|
| **ImageMagick** | `ProcessResize`, `ProcessImageRotate`, `ProcessWatermark`, `ProcessMontage`, `ProcessCrop`, `ProcessFormat`, `ProcessColor`, `ProcessSplice` | 图片缩放、旋转、水印、拼接、裁剪、格式转换、颜色处理 |
| **FFmpeg** | `ProcessVideoFormat`, `ProcessVideoCompress`, `ProcessVideoCover`, `ProcessVideoWatermark`, `ProcessVideoPreview` | 视频转码、压缩、封面提取、水印、预览生成 |
| **FFprobe** | `GetVideoMeta`, `GetVideoDuration`, `GetVideoResolution` | 视频元数据查询（时长、分辨率、码率） |
| **ExifTool** | `ProcessExifInfo`, `ProcessClearLocation`, `ProcessAudioMeta`, `ProcessAudioCover` | EXIF 读取、GPS 清除、音频元数据、封面提取 |

### 8.3 外部依赖清单

| 依赖 | 类型 | 用途 |
|------|------|------|
| **ImageMagick** | CLI 工具 | 图片处理核心引擎 |
| **FFmpeg / FFprobe** | CLI 工具 | 视频/音频处理核心引擎 |
| **ExifTool** | CLI 工具 | 元数据读写 |
| **COS (S3-compatible)** | 云存储 SDK | 文件上传/下载/元数据管理 |
| **Redis** | 数据库 | 服务注册发现、任务存储、分布式锁 |
| **MongoDB** | 数据库 | 区域地理数据存储（仅 region_server） |
| **Codis** | 内部服务 | 配置中心、文件元数据 |
| **IMS Gateway** | 内部框架 | HTTP 服务、IMJ 协议、服务发现 |
| **Turbo TimerWheel** | 内部库 | 定时任务调度（WorkerManager） |

---

## 9. 服务发现与集群

### 9.1 插件节点注册

```
┌──────────────┐    每 5 秒注册      ┌──────────────┐
│ PluginServer │ ──────────────────▶ │   Codis      │
│  (本机:8082) │  PluginEndpoint     │  (Redis)     │
└──────────────┘                     └──────────────┘
                                          │
                                          │ 读取
                                          ▼
                                   ┌──────────────┐
                                   │ LambdaServer  │
                                   │ (路由网关)     │
                                   └──────────────┘
```

`PluginEndpoint` 上报的信息：
```go
type PluginEndpoint struct {
    Function  string    // 插件名
    Version   string    // 版本
    EndPoint  string    // http://host:port
    Path      string    // /v1/cos/lambda/invoke/{name}
    Timestamp int64     // 心跳时间
    State: {
        MaxConcurrent int  // 最大并发
        NowConcurrent int  // 当前并发
        CPUMetrics    CPUMetrics
        MemoryMetrics MemoryMetrics
    }
}
```

### 9.2 节点选择策略

```go
func selectPluginServer(ctx, function) (*PluginEndpoint, error) {
    // 1. 优先复用上次节点（减少迁移成本）
    if previousEndpoint != nil && previousEndpoint.IsValid() {
        return previousEndpoint
    }
    // 2. 一致性哈希（基于 upstream URL MD5）
    hashRing := NewConsistentHashRing(150) // 150 虚拟节点
    selectedNode := hashRing.GetNode(md5(upstreamUrl))
    // 3. 权重评分回退（CPU 70% + 内存 30%）
    bestNode := endpoints.MaxBy(scoreNode)
}
```

---

## 10. 插件实现模板

基于项目中的实际代码，提取通用插件模板：

### 10.1 参数定义

```go
// 每个插件定义自己的参数结构体
// tag 中的 key 对应 URL 路径中的短名称
type MyPluginParams struct {
    Width    int64  `plugin:"w"`     // URL: /plugin/w/100
    Height   int64  `plugin:"h"`     // URL: /plugin/h/100
    Quality  int64  `plugin:"q"`     // URL: /plugin/q/90
    Format   string `plugin:"fm"`    // URL: /plugin/fm/png
    Replace  bool   `plugin:"replace"` // URL: /plugin/replace/true
}
```

### 10.2 Lambda 实现

```go
type MyPluginLambda struct {
    ctx    context.Context  // 生命周期上下文（用于取消、超时）
    hcli   *http.Client     // HTTP 客户端（下载上游资源）
    cosCli *sdk.CosSdk      // COS SDK（文件操作）
    // 其他依赖...
}

func NewMyPluginLambda(ctx context.Context, hcli *http.Client, cosCli *sdk.CosSdk) *MyPluginLambda {
    return &MyPluginLambda{ctx: ctx, hcli: hcli, cosCli: cosCli}
}

func (l *MyPluginLambda) Invoke(ctx *plugins.PluginContext[MyPluginParams]) error {
    worker := ctx.PluginTask()              // 获取任务信息
    params := ctx.GetExpressionParams()     // 获取类型化参数
    cctx := ctx.ParentCtx()                 // 获取生命周期上下文

    // 1. 查询文件信息
    fileInfo, err := l.cosCli.QueryFileInfo(cctx, worker.GetFileId())

    // 2. 获取源文件（共享缓存 + 原子下载）
    origin := utils.BuildOriginFilePath(fileInfo, fileInfo.GetExt())
    if err = utils.AcquireOriginFile(cctx, l.cosCli, worker.GetFileId(), origin); err != nil {
        return err
    }
    defer utils.ReleaseOriginFile(origin)

    // 3. 处理（调用 process 包函数）
    output := utils.BuildTempFilePath(".jpg")
    defer utils.CleanupTempFile(output)
    if err = process.ProcessResize(cctx, origin, output, &proto.ResizeParam{
        Width:  params.Width,
        Height: params.Height,
    }); err != nil {
        return err
    }

    // 4. 返回结果（流式输出）
    return l.Response(ctx.GetEcho(), worker, fileInfo, output)
}
```

### 10.3 响应处理（支持 Process/Submit 两种模式）

```go
func (l *MyPluginLambda) Response(ctx echo.Context, worker *plugins.PluginTask,
    fileInfo *cosPro.CosFile, result string) error {

    switch worker.GetTaskAction() {
    case plugins.PluginTaskActions.Process:
        // 同步模式: 直接流式返回文件
        info, fileType, _ := utils.GetFileInfo(result)
        f, _ := os.Open(result)
        defer f.Close()
        return httpx.StreamResponse(ctx, fileType, imjson.Codes.Success, f,
            httpx.WithHeaderOption("Content-Length", fmt.Sprintf("%d", info.Size())))

    case plugins.PluginTaskActions.Submit:
        // 异步模式: 上传结果 + 触发回调
        newFid, _ := l.replaceOriginalFile(fileInfo, result)
        return nil // 框架会自动 webhook 通知

    default:
        return errors.New("unknown task action")
    }
}
```

---

## 11. 资源管理与生命周期

### 11.1 临时文件管理

| 路径 | 用途 | 清理时机 |
|------|------|----------|
| `/tmp/plugins/{uuid}.{ext}` | 插件输出文件 | `defer CleanupTempFile()` |
| `/tmp/plugins/{md5}.{ext}` | 共享源文件 | 引用计数归零时 |
| `{plugin_outputBaseDir}/` | 插件专属输出 | TaskMonitor 定期清理 |

### 11.2 TaskMonitor（后台扫描器）

```go
// 插件项目还包含一个独立的 TaskMonitor，用于:
// 1. 定期扫描 COS 文件，发现有新文件时触发插件处理
// 2. 扫描 Redis 中 processing 状态超时的任务
// 3. 通过 Webhook 通知业务方结果
// 4. 自动上传处理结果到 COS
```

---

## 12. 关键设计模式

### 12.1 泛型接口模式

利用 Go 1.18+ 泛型实现**类型安全的插件参数**：
- 框架通过反射自动解析字符串参数到强类型结构体
- 编译时类型检查，运行时零额外开销
- 每个插件独立定义自己的参数类型

### 12.2 通道信号量模式

```go
// 用有缓冲 channel 实现轻量级并发控制
concurrentLimiter := make(chan int, maxConcurrent)

// 获取令牌（阻塞）
concurrentLimiter <- 1
defer func() { <-concurrentLimiter }()
```

优势：轻量、无锁、支持 context cancel、自动 FIFO

### 12.3 FutureTask 模式

```go
// 使用 turbo.FutureTask 包装异步执行
actual, loaded := l.taskStorage.SubmitPluginTask(task)
if !loaded {
    go func() {
        defer func() { <-pl.concurrentLimiter }()
        actual.f.Run() // 后台执行
    }()
    actual.f.Get() // 同步等待（Process 模式）
}
```

支持：cancel、timeout、状态查询、重试

### 12.4 引用计数文件缓存

```go
// AcquireOriginFile: 多插件共享同一源文件
// - 全局 map[path]*state 记录每个文件的引用数和等待者
// - 第一个下载者执行下载，后续调用方等待
// - 所有引用释放后删除本地文件
```

---

## 13. 从零设计插件服务的核心决策点

基于以上分析，设计一个可扩展的插件服务需要考虑：

### 13.1 必须确定的

| 决策点 | 选项 | 本项目选择 |
|--------|------|-----------|
| **插件接口风格** | 函数式 / 接口式 / 装饰器 | 泛型接口 `IPluginProvider[T]` |
| **参数传递方式** | JSON body / URL path / Query | URL path (`/k/v/k/v`) |
| **类型安全** | 动态 map / 反射解析 / 代码生成 | 反射 + struct tag |
| **并发控制** | 全局 / 按插件 / 按资源 | 每个插件独立信号量 |
| **生命周期** | 同步 / 异步 + 回调 | 双模式（Process/Submit） |
| **服务发现** | 静态配置 / DNS / 注册中心 | Redis 心跳注册 + 一致性哈希 |
| **协议层** | 纯 HTTP / 内部 RPC / IMJ | HTTP + IMJ 双协议 |

### 13.2 推荐架构

```
┌──────────────────────────────────────────────────────────┐
│                      网关层 (Gateway)                       │
│  - 认证鉴权、限流、路由                                    │
│  - 协议转换 (HTTP ↔ 内部协议)                               │
└──────────────────────┬───────────────────────────────────┘
                       │
┌──────────────────────▼───────────────────────────────────┐
│                    调度层 (Scheduler)                       │
│  - 服务发现: 插件节点注册与心跳                              │
│  - 负载均衡: 一致性哈希 / 权重 / 亲和性                       │
│  - 流量控制: 全局/插件级限流                                  │
│  - 任务编排: DAG 工作流 (可选)                               │
└──────────────────────┬───────────────────────────────────┘
                       │
┌──────────────────────▼───────────────────────────────────┐
│                    执行层 (Runner)                          │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐                   │
│  │ Plugin A │  │ Plugin B │  │ Plugin C │                   │
│  │ (实例1)  │  │ (实例1)  │  │ (实例1)  │                   │
│  └─────────┘  └─────────┘  └─────────┘                   │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐                   │
│  │ Plugin A │  │ Plugin B │  │ Plugin C │                   │
│  │ (实例2)  │  │ (实例2)  │  │ (实例2)  │                   │
│  └─────────┘  └─────────┘  └─────────┘                   │
│                                                           │
│  每个实例:                                                 │
│  - 并发信号量 (per-plugin channel)                          │
│  - 本地任务存储 (in-process LRU)                            │
│  - 外部工具管理 (进程池 / 复用)                               │
└───────────────────────────────────────────────────────────┘
```

### 13.3 插件的最小实现

一个插件只需要：

1. **参数结构体**（定义输入）
2. **实现 Invoke 方法**（定义处理逻辑）
3. **注册到框架**（绑定路径、并发、分组）

框架自动处理：路由、参数解析、并发控制、生命周期、注册发现、响应输出。

---

## 14. 项目代码组织

```
lambda-plugins/
├── bootstrap.go              # 入口: 初始化依赖、注册插件、启动服务
├── build.sh                  # 跨平台编译脚本
├── conf/                     # 配置目录
│   └── {env}/plugins.toml   # Redis、端口、工具路径
│       auto_policy.json      # 转码策略
│       region.json           # 地理区域数据
├── plugins/                  # 业务插件层
│   ├── image_*.go            # 图片处理插件 (12个)
│   ├── video_*.go            # 视频处理插件 (5个)
│   ├── file_cover.go         # 通用封面提取
│   ├── auto_policy.go        # 自动转码策略
│   ├── media_attribute.go    # 媒体属性
│   ├── task_monitor.go       # 后台任务扫描器
│   ├── utils/                # 通用工具
│   │   ├── utils.go          # 临时文件、下载、meta 操作
│   │   ├── origin_manager.go # 源文件共享缓存（引用计数）
│   │   └── gpu_utils.go      # GPU 检测与并发计算
│   └── process/              # 外部 CLI 工具封装层
│       ├── tools_path.go     # 工具路径初始化 + 可用性检测
│       ├── process_magick.go # ImageMagick 封装
│       ├── process_ffmpeg.go # FFmpeg 封装
│       ├── process_ffprobe.go # FFprobe 封装
│       └── process_exiftool.go # ExifTool 封装
├── server/
│   └── region_server.go      # 区域地理服务（Redis + 高德 API）
├── proto/
│   └── logger.go             # 分类日志定义
├── doc/                      # 插件说明书（markdown）
├── locale/                   # 国际化文案
└── shell/                    # 运行脚本
```

---

## 15. 日志体系

项目使用统一的分类日志，禁止裸 `log.Infof`：

```
Component|Method|Call|Level|Message|key=value...

// 6 种日志分类:
// - gateway:    接口访问、路由、中间件
// - operation:  最终操作事实（上传、下载、创建、删除）
// - scheduler:  定时任务、队列消费
// - integration: 第三方依赖调用（COS、CLI工具、HTTP）
// - business:   核心业务处理
// - system:     服务启动、配置加载、依赖初始化
```

---

## 16. 总结：插件服务的核心能力

| 能力 | 实现方式 | 说明 |
|------|----------|------|
| **插件注册** | 泛型函数 `WithPlugin[T]` | 声明式注册，自动路由 |
| **参数解析** | 反射 + struct tag | URL path → 强类型参数 |
| **并发控制** | Channel 信号量 | 每个插件独立限额 |
| **生命周期** | Submit/Process/Cancel | 同步/异步双模式 |
| **文件获取** | 共享缓存 + 引用计数 | 避免重复下载 |
| **结果输出** | 流式响应 / JSON / SSE | 支持多种返回格式 |
| **服务发现** | Redis 心跳注册 | 节点自动上下线 |
| **负载均衡** | 一致性哈希 + 权重 | 亲和性 + 公平性 |
| **任务恢复** | Redis + WorkerManager | 失败自动重试 |
| **结果通知** | Webhook 回调 | 异步任务完成通知 |
| **外部工具** | CLI 封装层 | ImageMagick, FFmpeg, ExifTool |
| **优雅关闭** | signal hook + 任务清理 | 不丢失进行中的任务 |
