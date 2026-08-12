# tools/email

SMTP 邮件发送工具，封装 gomail 为简洁的 API。

## 快速开始

```go
import "github.com/Effortful-lion/unibase/tools/email"

// 创建发送器
sender := email.New(
    "smtp.gmail.com",
    587,
    "user@gmail.com",
    "your-app-password",
    "noreply@example.com",
)

// 发送纯文本邮件
err := sender.Send("to@example.com", "Hello", "This is a plain text email.")

// 发送 HTML 邮件
err := sender.SendHTML("to@example.com", "Welcome!", "<h1>Welcome!</h1><p>Hello.</p>")

// 发送带附件的邮件
err := sender.SendWithFile(
    "to@example.com",
    "Monthly Report",
    "Please see the attached report.",
    "text/html",
    []string{"report.pdf", "data.xlsx"},
)
```

## API

### New

```go
func New(host string, port int, username, password, from string) *Sender
```

创建邮件发送器。单个 Sender 实例线程安全，可被多个 goroutine 并发使用。

| 参数 | 说明 |
|------|------|
| host | SMTP 服务器地址（如 `"smtp.gmail.com"`） |
| port | SMTP 端口（SSL: 465，STARTTLS: 587） |
| username | 认证用户名 |
| password | 认证密码或授权码 |
| from | 发件人地址（`"Name <email>"` 或 `"email"`） |

### Send / SendHTML / SendWithFile

```go
func (s *Sender) Send(to, subject, body string) error
func (s *Sender) SendHTML(to, subject, htmlBody string) error
func (s *Sender) SendWithFile(to, subject, body, bodyType string, files []string) error
```

## 邮件地址工具

```go
// 解析地址
addr, err := email.ParseAddress("Alice <alice@example.com>")

// 格式化地址
formatted := email.FormatAddress(addr) // "Alice <alice@example.com>"

// 连接多个地址
recipients := email.JoinAddresses("a@ex.com", "b@ex.com")
```

## 注意事项

- 连接超时默认 10 秒，首次发送时建立连接并复用
- Gmail 需使用"应用专用密码"（App Password），不能使用普通登录密码
- QQ 邮箱 SMTP 端口为 465（SSL），需开启 POP3/SMTP 服务

## 依赖

- `gopkg.in/gomail.v2`
