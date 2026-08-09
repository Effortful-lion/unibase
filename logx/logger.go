package logx

import (
	"fmt"
	"maps"
	"os"
	"runtime"
	"strconv"
	"time"
)

// Logger 是面向用户使用的日志记录器。
//
// 用法示例：
//
//	// 直接使用
//	lg.Info("服务启动", lg.Fields{"port": 8080})
//	lg.Error("连接失败", lg.Fields{"db": "mysql"})
//
//	// 模块化使用
//	lg.Module("user").Info("用户登录", lg.Fields{"uid": 123})
//	lg.Module("shop").Warn("库存不足", lg.Fields{"sku": "A001"})
//
//	// 使用具体注册的日志器
//	userLog := lg.New(userWriter).Module("user")
//	userLog.Info("用户登录成功")
type Logger struct {
	module        string                  // 所属模块
	writer        Writer                  // 输出目标
	fields        Fields                  // 预置的固定字段
	callerSkip    int                     // 调用栈跳过层数
	disableCaller bool                    // 是否关闭 caller 文件位置采集
	fatalHook     func(entry *Entry) bool // Fatal 回调：返回 true 表示已处理，不再 os.Exit
}

// New 创建一个日志记录器。
func New(writer Writer) *Logger {
	if writer == nil {
		writer = NewConsoleWriter(os.Stdout, LevelInfo)
	}
	return &Logger{
		writer:     writer,
		callerSkip: 2, // New → Debug/Info... → caller
	}
}

// DisableCaller 关闭 caller 文件位置采集，减少 runtime.Caller 开销。
// 返回 *Logger 以便链式调用。
func (l *Logger) DisableCaller() *Logger {
	l.disableCaller = true
	return l
}

// SetFatalHook 设置 Fatal 日志的回调。
// hook 接收日志 entry，返回 true 表示已处理，Logger 不再调用 os.Exit(1)。
// 返回 *Logger 以便链式调用。
func (l *Logger) SetFatalHook(hook func(entry *Entry) bool) *Logger {
	l.fatalHook = hook
	return l
}

// Module 创建一个绑定到指定模块的子 Logger。
// 该 Logger 的所有日志都会带上模块名。
//
// 子 Logger 创建时复制父 Logger 的 writer 和 fields，
// 后续父 Logger 的 writer 变化不会影响子 Logger。
func (l *Logger) Module(module string) *Logger {
	return &Logger{
		module:     module,
		writer:     l.writer,
		fields:     l.fields, // 继承父 Logger 的固定字段
		callerSkip: l.callerSkip,
	}
}

// With 创建一个带固定字段的子 Logger。
// 固定字段会自动附加到每条日志中。
func (l *Logger) With(fields Fields) *Logger {
	merged := Fields{}
	maps.Copy(merged, l.fields)
	maps.Copy(merged, fields)
	return &Logger{
		module:     l.module,
		writer:     l.writer,
		fields:     merged,
		callerSkip: l.callerSkip,
	}
}

// Debug 输出调试日志。
func (l *Logger) Debug(msg string, fields ...Fields) {
	l.log(LevelDebug, msg, fields...)
}

// Info 输出信息日志。
func (l *Logger) Info(msg string, fields ...Fields) {
	l.log(LevelInfo, msg, fields...)
}

// Warn 输出警告日志。
func (l *Logger) Warn(msg string, fields ...Fields) {
	l.log(LevelWarn, msg, fields...)
}

// Error 输出错误日志。
func (l *Logger) Error(msg string, fields ...Fields) {
	l.log(LevelError, msg, fields...)
}

// Fatal 输出致命日志并退出程序。
func (l *Logger) Fatal(msg string, fields ...Fields) {
	entry := l.log(LevelFatal, msg, fields...)
	if l.fatalHook != nil && entry != nil {
		if l.fatalHook(entry) {
			return
		}
	}
	os.Exit(1)
}

// Debugf 格式化调试日志。
func (l *Logger) Debugf(format string, args ...any) {
	l.log(LevelDebug, fmt.Sprintf(format, args...))
}

// Infof 格式化信息日志。
func (l *Logger) Infof(format string, args ...any) {
	l.log(LevelInfo, fmt.Sprintf(format, args...))
}

// Warnf 格式化警告日志。
func (l *Logger) Warnf(format string, args ...any) {
	l.log(LevelWarn, fmt.Sprintf(format, args...))
}

// Errorf 格式化错误日志。
func (l *Logger) Errorf(format string, args ...any) {
	l.log(LevelError, fmt.Sprintf(format, args...))
}

// Fatalf 格式化致命日志并退出程序。
func (l *Logger) Fatalf(format string, args ...any) {
	entry := l.log(LevelFatal, fmt.Sprintf(format, args...))
	if l.fatalHook != nil && entry != nil {
		if l.fatalHook(entry) {
			return
		}
	}
	os.Exit(1)
}

// log 是内部统一的日志写入方法。
func (l *Logger) log(level Level, msg string, fields ...Fields) *Entry {
	w := l.writer
	if w == nil {
		return nil
	}
	if level < w.Level() {
		return nil
	}

	// 合并字段
	merged := Fields{}
	for k, v := range l.fields {
		merged[k] = v
	}
	for _, f := range fields {
		for k, v := range f {
			merged[k] = v
		}
	}

	var file string
	if !l.disableCaller {
		file = caller(l.callerSkip)
	}

	entry := &Entry{
		Time:    time.Now(),
		Level:   level,
		Module:  l.module,
		File:    file,
		Message: msg,
		Fields:  merged,
	}

	_ = w.Write(entry)
	return entry
}

// caller 获取调用者的文件名和行号。
func caller(skip int) string {
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return ""
	}
	// 只保留文件名，不保留完整路径
	for i := len(file) - 1; i >= 0; i-- {
		if file[i] == '/' || file[i] == '\\' {
			file = file[i+1:]
			break
		}
	}
	return file + ":" + strconv.Itoa(line)
}

// ============================================================================
// 包级别便捷函数 — 使用默认 Logger
// ============================================================================

// defaultLogger 是包级别默认 Logger。
var defaultLogger = New(NewConsoleWriter(os.Stdout, LevelInfo))

// Default 返回默认 Logger。
func Default() *Logger {
	return defaultLogger
}

// SetDefault 替换默认 Logger。
// 注意：此方式不会更新已通过包级 Module() 创建的子 Logger。
// 推荐在程序启动时尽早调用，或使用 SetDefaultWriter 只修改 writer 字段。
func SetDefault(l *Logger) {
	defaultLogger = l
}

// Module 使用默认 Logger 创建模块子 Logger。
//
// 子 Logger 创建时复制 defaultLogger 的 writer 和 fields，
// 后续 defaultLogger 的 writer 变化不会影响子 Logger。
func Module(module string) *Logger {
	return &Logger{
		module:     module,
		writer:     defaultLogger.writer,
		fields:     defaultLogger.fields,
		callerSkip: 3, // Module → Debug/Info... → caller
	}
}

// ============================================================================
// 框架内置日志器
// ============================================================================

// Frame 是框架内部日志器，用于记录 llmLib 库自身的运行时信息。
// 所有库内部错误、警告都通过 Frame 输出，模块名为 "frame"。
var Frame = New(NewConsoleWriter(os.Stderr, LevelWarn)).Module("frame")

// SetFrameWriter 替换 Frame 日志器的输出目标。
func SetFrameWriter(w Writer) {
	Frame = New(w).Module("frame")
}

// ============================================================================
// 包级别便捷函数
// ============================================================================

// Debug 包级别 Debug。
func Debug(msg string, fields ...Fields) { defaultLogger.Debug(msg, fields...) }

// Info 包级别 Info。
func Info(msg string, fields ...Fields) { defaultLogger.Info(msg, fields...) }

// Warn 包级别 Warn。
func Warn(msg string, fields ...Fields) { defaultLogger.Warn(msg, fields...) }

// Error 包级别 Error。
func Error(msg string, fields ...Fields) { defaultLogger.Error(msg, fields...) }

// Fatal 包级别 Fatal。
func Fatal(msg string, fields ...Fields) { defaultLogger.Fatal(msg, fields...) }

// Debugf 包级别 Debugf。
func Debugf(format string, args ...any) { defaultLogger.Debugf(format, args...) }

// Infof 包级别 Infof。
func Infof(format string, args ...any) { defaultLogger.Infof(format, args...) }

// Warnf 包级别 Warnf。
func Warnf(format string, args ...any) { defaultLogger.Warnf(format, args...) }

// Errorf 包级别 Errorf。
func Errorf(format string, args ...any) { defaultLogger.Errorf(format, args...) }

// Fatalf 包级别 Fatalf。
func Fatalf(format string, args ...any) { defaultLogger.Fatalf(format, args...) }
