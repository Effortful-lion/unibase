// Package email 提供 SMTP 邮件发送能力。
//
// 基于 gomail，封装为简洁的发送器，支持 HTML 和纯文本邮件。
//
// 快速开始：
//
//	sender := email.New("smtp.gmail.com", 587, "user@gmail.com", "app-password", "noreply@example.com")
//	err := sender.SendHTML("to@example.com", "Subject", "<h1>Hello</h1>")
//
// 能力：New（创建发送器）、Send / SendHTML / SendWithFile。
package email

import (
	"net/mail"
	"strings"

	"gopkg.in/gomail.v2"
)

// Sender SMTP 邮件发送器。
//
// 线程安全：单个 Sender 实例可被多个 goroutine 并发使用。
// 底层使用连接池，无需反复创建 Dialer。
type Sender struct {
	host     string
	port     int
	username string
	password string
	from     string
	dialer   *gomail.Dialer
}

// New 创建邮件发送器。
//
// host: SMTP 服务器地址（如 "smtp.gmail.com"）。
// port: SMTP 端口（SSL 通常为 465，STARTTLS 为 587）。
// username: 认证用户名（通常与 from 相同）。
// password: 认证密码或授权码。
// from: 发件人地址（格式为 "Name <email>" 或纯邮箱地址）。
func New(host string, port int, username, password, from string) *Sender {
	d := gomail.NewDialer(host, port, username, password)

	return &Sender{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		dialer:   d,
	}
}

// Send 发送一封纯文本邮件。
func (s *Sender) Send(to, subject, body string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", s.from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)

	return s.dialer.DialAndSend(m)
}

// SendHTML 发送一封 HTML 邮件。
func (s *Sender) SendHTML(to, subject, htmlBody string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", s.from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", htmlBody)

	return s.dialer.DialAndSend(m)
}

// SendWithFile 发送一封带附件的邮件。
//
// bodyType: MIME 类型，如 "text/plain" 或 "text/html"。
// files: 附件文件路径列表。
func (s *Sender) SendWithFile(to, subject, body, bodyType string, files []string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", s.from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody(bodyType, body)

	for _, f := range files {
		m.Attach(f)
	}

	return s.dialer.DialAndSend(m)
}

// Dial 执行 SMTP 连接（惰性连接，首次发送时建立）。
//
// 通常不需要手动调用 Dial，Send/SendHTML 会自动处理。
// 仅在需要检查连接是否可用时使用。
func (s *Sender) Dial() error {
	_, err := s.dialer.Dial()
	return err
}

// ParseAddress 解析邮件地址字符串为 mail.Address。
//
// 支持格式：
//   - "user@example.com"
//   - "Name <user@example.com>"
func ParseAddress(addr string) (*mail.Address, error) {
	return mail.ParseAddress(addr)
}

// FormatAddress 将 mail.Address 格式化为字符串。
//
// 输出格式：`"Name" <user@example.com>` 或 `user@example.com`。
func FormatAddress(addr *mail.Address) string {
	if addr == nil {
		return ""
	}
	if addr.Name == "" {
		return addr.Address
	}
	return addr.String()
}

// JoinAddresses 用逗号连接多个邮件地址字符串。
func JoinAddresses(addrs ...string) string {
	return strings.Join(addrs, ", ")
}
