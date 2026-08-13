# httpx

Gin 框架增强工具库。封装常见能力、增强原生能力、不强绑业务。

## 设计原则

- **封装常见能力**：大家写 Gin 服务时反复要用的东西
- **原生能力不管**：`c.JSON`、`c.Query`、`c.Bind` 等 Gin 已有的，不重复包装
- **增强原生能力**：补 Gin 没有的，强集成 validator 等

## 安装

```bash
go get github.com/Effortful-lion/httpx
```

## 快速开始

```go
package main

import (
    "time"

    "github.com/gin-gonic/gin"
    "github.com/Effortful-lion/httpx"
    "github.com/Effortful-lion/httpx/params"
    "github.com/Effortful-lion/httpx/middleware"
)

func main() {
    r := gin.Default()

    // 中间件
    r.Use(middleware.CORS(middleware.WithAllowOrigins("http://localhost:3000")))
    r.Use(middleware.Metrics())
    r.Use(httpx.JWT([]byte("secret")))

    // 参数增强：类型转换 + 自动校验
    r.POST("/users", func(c *gin.Context) {
        var req CreateUserReq
        params.MustBindJSON(c, &req) // 绑定 + validate tag 校验，失败 Abort 400

        claims, _ := httpx.ClaimsFromContext(c)
        // ... 业务逻辑

        c.JSON(200, gin.H{"user_id": claims.UserID})
    })

    // SSE 推送
    r.GET("/events", func(c *gin.Context) {
        httpx.Gin(c).SSE(func(w httpx.SSEWriter) error {
            return w.WriteEvent(sse.Event{Data: []byte("hello")})
        })
    })

    r.Run(":8080")
}
```

## 能力详解

### 一、Gin 响应增强

Gin 原生的 `c.JSON`、`c.String`、`c.HTML` 直接用，httpx 补它没有的能力。

#### SSE 服务器推送

```go
func handler(c *gin.Context) {
    httpx.Gin(c).SSE(func(w httpx.SSEWriter) error {
        return w.WriteEvent(sse.Event{
            ID:    []byte("1"),
            Event: []byte("message"),
            Data:  []byte(`{"text":"hello"}`),
        })
    })
}
```

#### 流式响应

适合大文件下载、实时数据流等场景：

```go
func handler(c *gin.Context) {
    file, _ := os.Open("large.bin")
    defer file.Close()

    httpx.Gin(c).Stream(200, "application/octet-stream", file)
}
```

#### HTML / File

```go
func handler(c *gin.Context) {
    // HTML 响应
    httpx.Gin(c).HTML(200, []byte("<h1>hello</h1>"))

    // 文件下载
    httpx.Gin(c).File(200, "/path/to/file.pdf")
}
```

### 二、参数增强

Gin 原生的 `c.Query` 只返回 string，`c.ShouldBindJSON` 不集成校验。httpx 补上。

#### Query 参数类型转换

```go
func handler(c *gin.Context) {
    id    := c.QueryInt("id")                // int，不存在或解析失败返回 0
    flag  := c.QueryBool("active")           // bool
    page  := c.QueryIntDefault("page", 1)    // int，带默认值
    tags  := c.QuerySlice("tags")            // []string
    birth := c.QueryTime("birth", "2006-01-02") // time.Time
}
```

#### 绑定 + 自动校验

**方式一：struct tag 校验（推荐）**

定义 struct 时加 `validate` tag：

```go
type CreateUserReq struct {
    Name   string `json:"name"  validate:"required,min=1,max=100"`
    Email  string `json:"email" validate:"required,email"`
    Age    int    `json:"age"   validate:"gte=0,lte=150"`
}

func handler(c *gin.Context) {
    var req CreateUserReq
    params.MustBindJSON(c, &req) // 自动校验，失败直接 Abort 400
    // req 已校验通过，直接使用
}
```

**方式二：自定义校验规则（不依赖 struct tag）**

```go
func handler(c *gin.Context) {
    var req CreateUserReq
    params.MustBindWith(c, &req, binding.JSON,
        params.Rule("Name").Required().Min(1).Max(100),
        params.Rule("Email").Required().Email(),
    )
}
```

**注册自定义校验器**

项目初始化时注册一次，之后所有 Bind 系列函数自动生效：

```go
func init() {
    params.RegisterRule("mobile", func(fl validator.FieldLevel) bool {
        return regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(fl.Field().String())
    })
}

// 使用
type CreateUserReq struct {
    Mobile string `json:"mobile" validate:"required,mobile"`
}
```

**Fluent 规则构造器**

不依赖 struct tag，直接在代码中构造校验规则：

```go
func handler(c *gin.Context) {
    var req CreateUserReq
    params.MustBindWith(c, &req, binding.JSON,
        params.Rule("Name").Required().Min(1).Max(100),
        params.Rule("Email").Required().Email(),
    )
}
```

`Rule.Custom` 引用已注册的 tag，打通两套用法：

```go
func handler(c *gin.Context) {
    var req CreateUserReq
    params.MustBindWith(c, &req, binding.JSON,
        params.Rule("Mobile").Required().Custom("mobile"),
    )
}
```

### 三、JWT

#### 一行搞定（HMAC 对称密钥）

```go
r.Use(httpx.JWT([]byte("secret")))
```

#### 自定义解析器（RSA / 密钥轮转）

```go
parser := httpx.NewHMACParser([]byte("secret"))
r.Use(httpx.JWT(httpx.WithParser(parser)))
```

#### 业务中提取 Claims

```go
func handler(c *gin.Context) {
    claims, err := httpx.ClaimsFromContext(c)
    if err != nil {
        c.JSON(401, gin.H{"error": "unauthorized"})
        return
    }
    // claims.UserID / claims.Username / claims.Role
}
```

#### 手动生成/验证 Token

```go
parser := httpx.NewHMACParser([]byte("secret"))

// 生成
token, err := parser.Sign(httpx.NewClaims("uid-123", "admin", 2*time.Hour), secret)

// 验证
claims, err := parser.Parse(token)
valid := parser.Verify(token)
```

### 四、中间件

#### CORS

```go
r.Use(middleware.CORS(
    middleware.WithAllowOrigins("http://localhost:3000", "https://example.com"),
    middleware.WithAllowMethods("GET", "POST", "PUT", "DELETE"),
    middleware.WithAllowHeaders("Origin", "Content-Type", "Authorization"),
    middleware.WithAllowCredentials(true),
    middleware.WithMaxAge(3600),
))
```

#### 限流

```go
limiter := middleware.NewRateLimiter(rate.Limit(10), 100) // 每秒 10 个 token，桶容量 100
r.Use(middleware.RateLimit(limiter))

// 自定义限流 key（默认按 IP）
r.Use(middleware.RateLimit(limiter,
    middleware.WithRateLimitKey(func(c *gin.Context) string {
        return c.GetHeader("X-API-Key") // 按 API Key 限流
    }),
))
```

`RateLimiter` 提供 `Stop()` 方法，用于优雅关闭时停止清理 goroutine：

```go
defer limiter.Stop()
```

#### Prometheus 指标

```go
r.Use(middleware.Metrics())

// 暴露 metrics 端点
r.GET("/metrics", middleware.MetricsHandler())
```

支持自定义命名空间和子系统：

```go
r.Use(middleware.Metrics(
    middleware.WithNamespace("myapp"),
    middleware.WithSubsystem("api"),
))
```

#### Panic 恢复

```go
r.Use(middleware.Panic())
```

捕获 handler 中的 panic，记录错误日志并返回 500 响应，避免进程崩溃。

```go
r.Use(middleware.Panic(middleware.WithPanicLogger(myLogger)))
```

### 五、服务启动与优雅关闭

```go
import "github.com/Effortful-lion/httpx"

r := gin.Default()

if err := httpx.RunWithShutdown(r, ":8080",
    httpx.WithShutdownTimeout(15*time.Second),
    httpx.WithShutdownHook(func(ctx context.Context) error {
        return db.Close() // 关闭数据库连接
    }),
); err != nil {
    log.Fatal(err)
}
```

监听 SIGTERM / SIGINT 信号，优雅关闭流程：
1. 执行 `WithShutdownHook` 钩子（串行）
2. 停止接受新请求，等待活跃请求完成
3. 超时则强制退出

开发环境可直接使用 `httpx.Run`（无优雅关闭）：

```go
httpx.Run(r, ":8080")
```

### 六、HTTP 客户端

和 Gin 无关的独立能力，Builder 链式调用：

```go
// GET
res := httpx.Get().URL("https://api.example.com/users").Query("id", "123").Do(ctx)
if err := res.Err(); err != nil {
    // httpx.IsTimeout(err) / httpx.IsClientError(err) / httpx.IsStatus(err, 404)
    return
}
var users []User
res.JSON(&users)

// POST JSON
res := httpx.Post().URL("https://api.example.com/users").JSON(body).Do(ctx)

// 带配置
res := httpx.Get().
    URL("https://api.example.com/slow").
    Header("Authorization", "Bearer xxx").
    Timeout(5 * time.Second).
    Retry(3).
    Do(ctx)

// 响应读取
bytes, _ := res.Bytes()
text, _  := res.Text()
res.JSON(&result)
reader, _ := res.Stream()  // 大文件，调用方负责关闭
res.SaveTo("/tmp/download.bin")
```

### 六、断点续传

```go
func handler(c *gin.Context) {
    file, _ := os.Open("video.mp4")
    defer file.Close()

    stat, _ := file.Stat()
    totalSize := stat.Size()

    // 解析 Range 请求
    ranges, err := ranger.ParseRange(c.GetHeader("Range"), totalSize)
    if err != nil || len(ranges) == 0 {
        c.File("video.mp4")
        return
    }

    // 返回部分内容
    r := ranges[0]
    file.Seek(r.Start, io.SeekStart)
    c.Header("Content-Range", ranger.ContentRange(r))
    c.Header("Accept-Ranges", "bytes")
    c.Status(http.StatusPartialContent)
    io.CopyN(c.Writer, file, r.End-r.Start+1)
}
```

## 子包一览

| 子包 | 说明 | Gin 依赖 |
|------|------|----------|
| `httpx` | 根包：`Gin(c)` / `JWT(secret)` / `NewHMACParser` / `NewClaims` / `RunWithShutdown` | 是 |
| `httpx/params` | 参数增强：`QueryInt` / `MustBindJSON` / `RegisterRule` | 是 |
| `httpx/response` | 响应增强：Stream / SSE / HTML / File | 是 |
| `httpx/jwt` | JWT：Parser / Claims / 中间件 / ClaimsFromContext | 否 |
| `httpx/sse` | SSE：Event / Writer | 否 |
| `httpx/ranger` | Range 解析 / 断点续传 | 否 |
| `httpx/client` | HTTP 客户端：Builder 模式 | 否 |
| `httpx/middleware` | CORS / 限流 / Prometheus 指标 / Panic 恢复 | 是 |
| `httpx/swagger` | Swagger UI 挂载：`Setup(r, basePath, specURL)` | 是 |
| `httpx/captcha` | 图形验证码：`Generate()` / `Verify(id, answer)` | 是 |

## Swagger UI 集成

一行代码挂载 Swagger UI：

```go
import _ "yourProject/docs" // swag init 生成的 docs 包
import "github.com/Effortful-lion/httpx/swagger"

func main() {
    r := gin.Default()
    swagger.Setup(r, "/api/v1", "/swagger/doc.json")
    r.Run(":8080")
}
```

访问 `http://localhost:8080/api/v1/swagger/index.html` 查看文档。

前置条件：项目需使用 [swaggo/swag](https://github.com/swaggo/swag) 生成 docs：

```bash
swag init -g cmd/main.go -o docs
```

## 图形验证码

```go
import "github.com/Effortful-lion/httpx/captcha"

// 生成验证码
result := captcha.Generate()

// 返回给前端
c.JSON(200, gin.H{
    "captcha_id": result.ID,
    "captcha_img": "data:image/png;base64," + result.Image,
})

// 校验（验证码 ID 通过 Cookie 带回）
if captcha.Verify(captchaID, userInput) {
    // 校验通过
}
```

支持自定义配置：

```go
result := captcha.Generate(
    captcha.WithHeight(100),
    captcha.WithWidth(300),
    captcha.WithLength(4),
)
```

## 错误处理

### 客户端错误

```go
res := httpx.Get().URL("...").Do(ctx)
if err := res.Err(); err != nil {
    if httpx.IsTimeout(err) { ... }
    if httpx.IsClientError(err) { ... }  // 4xx
    if httpx.IsServerError(err) { ... }  // 5xx
    if httpx.IsStatus(err, 404) { ... }
}
```

### 参数校验错误

```go
params.MustBindJSON(c, &req) // 失败时自动 Abort 400，返回格式：
// {"error": "Name: is required; Email: must be a valid email"}
```
