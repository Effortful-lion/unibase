// Package email 提供 SMTP 邮件发送能力。
//
// 基于 gomail 封装，支持纯文本、HTML 和带附件的邮件发送。
// 单个 Sender 实例线程安全，可被多个 goroutine 并发使用。
//
// 快速开始：
//
//	sender := email.New("smtp.gmail.com", 587, "user@gmail.com", "app-password", "noreply@example.com")
//
//	// 纯文本
//	sender.Send("to@example.com", "Subject", "Hello")
//
//	// HTML
//	sender.SendHTML("to@example.com", "<h1>Hello</h1>", "body.html")
//
//	// 带附件
//	sender.SendWithFile("to@example.com", "Report", "Please see attached", "text/html", []string{"report.pdf"})
//
// 能力：New（创建发送器）、Send（纯文本）、SendHTML（富文本）、SendWithFile（带附件）。
package email
