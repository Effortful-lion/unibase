# httpx/captcha

图形验证码生成与校验，基于 mojocn/base64Captcha。

## 快速开始

```go
import "github.com/Effortful-lion/httpx/captcha"

// 生成验证码
result := captcha.Generate()
// result.ID  — 验证码 ID（通过 Cookie 发送给前端）
// result.Image — base64 图片（嵌入 <img>）

// 校验
if captcha.Verify(result.ID, userInput) {
    // 校验通过
}
```

## 自定义配置

```go
import "github.com/Effortful-lion/httpx/captcha"

result := captcha.Generate(
    captcha.WithHeight(100),
    captcha.WithWidth(300),
    captcha.WithLength(4),
    captcha.WithNoiseCount(2),
)
```

## 中间件模式（推荐）

验证码通常配合 Gin 路由使用：

```go
r.GET("/captcha", func(c *gin.Context) {
    result := captcha.Generate()
    c.SetCookie("captcha_id", result.ID, 300, "/", "", false, true)
    c.JSON(200, gin.H{
        "captcha_id": result.ID,
        "captcha_img": "data:image/png;base64," + result.Image,
    })
})
```

## API

| 函数 | 说明 |
|------|------|
| `Generate(opts ...func(*Config)) *Result` | 生成验证码 |
| `Verify(id, answer string, clear ...bool) bool` | 校验验证码 |
| `WithHeight(h int) func(*Config)` | 设置图片高度 |
| `WithWidth(w int) func(*Config)` | 设置图片宽度 |
| `WithLength(l int) func(*Config)` | 设置字符长度 |
| `WithNoiseCount(n int) func(*Config)` | 设置干扰线数量 |

## Result 结构

| 字段 | 说明 |
|------|------|
| `ID` | 验证码唯一标识 |
| `Image` | base64 编码的图片（无前缀） |
| `Answer` | 正确答案（仅开发/测试用） |

## 注意事项

- 生产环境请勿使用 Result.Answer，应通过 Cookie/Session 关联 ID 与用户
- DefaultMemStore 适用于单进程部署；多副本场景需替换为 Redis Store
- 验证码字符集已去除 `0/O/1/I/l` 等易混淆字符

## 依赖

- `github.com/mojocn/base64Captcha`
