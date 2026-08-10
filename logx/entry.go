package logx

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"
)

// Entry 是一条日志记录，包含完整的上下文信息。
type Entry struct {
	Time    time.Time // 日志时间
	Level   Level     // 日志级别
	Module  string    // 模块/子系统名称，如 "user", "shop", "order"
	File    string    // 调用位置，如 "service.go:42"
	Message string    // 日志内容
	Fields  Fields    // 结构化字段
}

// Fields 是结构化日志字段，用于携带业务上下文。
type Fields map[string]any

// Format 返回 Entry 的默认字符串表示。
func (e *Entry) Format() string {
	loc := ""
	if e.File != "" {
		loc = " " + e.File
	}
	mod := ""
	if e.Module != "" {
		mod = "[" + e.Module + "] "
	}
	msg := fmt.Sprintf("[%s] %s%s%s", e.Level.String(), mod, e.Message, loc)
	if len(e.Fields) > 0 {
		msg += " " + e.Fields.format()
	}
	return msg
}

// FormatJSON 将 Entry 序列化为扁平 JSON 字节数组。
// 输出示例：{"time":"2026-08-10T12:00:00Z","level":"INFO","module":"user","file":"app.go:42","message":"hello","uid":123}
func (e *Entry) FormatJSON() ([]byte, error) {
	m := make(map[string]any, 6+len(e.Fields))
	m["time"] = e.Time.Format(time.RFC3339)
	m["level"] = e.Level.String()
	m["message"] = e.Message
	if e.Module != "" {
		m["module"] = e.Module
	}
	if e.File != "" {
		m["file"] = e.File
	}
	maps.Copy(m, e.Fields)
	return json.Marshal(m)
}
func (f Fields) format() string {
	if len(f) == 0 {
		return ""
	}
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(fmt.Sprintf("%s=%v", k, f[k]))
	}
	return b.String()
}
