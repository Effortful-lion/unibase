package logx

import (
	"fmt"
	"strings"
)

// Level 表示日志级别，数值越小越严重。
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// String 返回日志级别的字符串表示。
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return fmt.Sprintf("LEVEL(%d)", l)
	}
}

// ParseLevel 从字符串解析日志级别，不区分大小写。
func ParseLevel(s string) (Level, error) {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return LevelDebug, nil
	case "INFO":
		return LevelInfo, nil
	case "WARN", "WARNING":
		return LevelWarn, nil
	case "ERROR":
		return LevelError, nil
	case "FATAL":
		return LevelFatal, nil
	default:
		return LevelInfo, fmt.Errorf("lg: unknown level %q", s)
	}
}
