package logx

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Writer 是日志输出接口。实现此接口即可将日志输出到任意目标。
//
// 典型实现：
//   - os.Stdout / os.Stderr
//   - 文件
//   - 网络（UDP/TCP）
//   - 消息队列
//   - 第三方日志服务
type Writer interface {
	// Write 写入一条日志记录。
	Write(entry *Entry) error

	// Level 返回该 Writer 接受的最低日志级别。
	// 低于此级别的日志不会被传递给 Write。
	Level() Level

	// Close 关闭 Writer，释放资源。
	Close() error
}

// Formatter 将 Entry 格式化为字符串。
// 实现此接口可自定义日志输出格式。
type Formatter interface {
	Format(entry *Entry) string
}

// JSONFormatter 内置 JSON 格式化器。
// 用法：writer.SetFormatter(lg.JSONFormatter)
var JSONFormatter Formatter = jsonFormatter{}

type jsonFormatter struct{}

func (jsonFormatter) Format(e *Entry) string {
	b, err := e.FormatJSON()
	if err != nil {
		return fmt.Sprintf(`{"error":"json marshal failed","message":%q}`, e.Message)
	}
	return string(b)
}

// ============================================================================
// ConsoleWriter — 控制台输出
// ============================================================================

// ConsoleWriter 将日志写入 io.Writer（通常为 os.Stdout 或 os.Stderr）。
type ConsoleWriter struct {
	out       io.Writer
	level     Level
	formatter Formatter
	mu        sync.Mutex
}

// NewConsoleWriter creates a console writer.
//
// out: output target, such as os.Stdout or os.Stderr
// level: minimum log level
func NewConsoleWriter(out io.Writer, level Level) *ConsoleWriter {
	if out == nil {
		out = os.Stdout
	}
	return &ConsoleWriter{out: out, level: level}
}

// SetFormatter 设置格式化器。nil 表示使用默认文本格式。
func (w *ConsoleWriter) SetFormatter(f Formatter) {
	w.formatter = f
}

// Level 返回该 Writer 接受的最低日志级别。
func (w *ConsoleWriter) Level() Level { return w.level }

// formatLine 将 Entry 格式化为一行文本。
func (w *ConsoleWriter) formatLine(entry *Entry) string {
	if w.formatter != nil {
		return w.formatter.Format(entry)
	}
	return entry.Format()
}

// Write 写入一条日志记录。
// 低于 w.level 的日志会被过滤。
func (w *ConsoleWriter) Write(entry *Entry) error {
	if entry.Level < w.level {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := fmt.Fprintln(w.out, w.formatLine(entry))
	return err
}

// Close 关闭 Writer，释放资源。
func (w *ConsoleWriter) Close() error { return nil }

// ============================================================================
// LogNamePattern — 日志文件名链式构建器
// ============================================================================

// LogMeta 提供日志文件名生成所需的元信息。
type LogMeta struct {
	Module string    // 模块名
	Time   time.Time // 日志时间
}

// LogName 日志文件名生成函数。接收 LogMeta，返回文件名（不含目录和扩展名）。
type LogName func(meta LogMeta) string

// nameSegment 文件名片段生成函数。
type nameSegment func(meta LogMeta) string

// LogNamePattern 日志文件名模式构建器，链式组合各个最小片段，Build() 自动追加 .log 后缀。
//
// 使用示例:
//
//	// pg_2026-08-01.log
//	NewLogNamePattern().Module().Char("_").Date("2006-01-02").Build()
//
//	// pg_2026-08-01_12-30-00.log
//	NewLogNamePattern().Module().Char("_").Date("2006-01-02").Char("_").Clock("15-04-05").Build()
//
//	// 2026-08-01_pg.log
//	NewLogNamePattern().Date("2006-01-02").Char("_").Module().Build()
//
//	// 1704067200_pg.log
//	NewLogNamePattern().Timestamp().Char("_").Module().Build()
type LogNamePattern struct {
	segs []nameSegment
}

// NewLogNamePattern 创建文件名模式构建器。
func NewLogNamePattern() *LogNamePattern {
	return &LogNamePattern{}
}

// Module 追加模块名片段。
func (p *LogNamePattern) Module() *LogNamePattern {
	p.segs = append(p.segs, func(meta LogMeta) string { return meta.Module })
	return p
}

// Date 追加日期片段，layout 为 Go 时间格式，如 "2006-01-02"。
func (p *LogNamePattern) Date(layout string) *LogNamePattern {
	p.segs = append(p.segs, func(meta LogMeta) string { return meta.Time.Format(layout) })
	return p
}

// Clock 追加时钟片段，layout 如 "15-04-05"、"15:04:05"。
func (p *LogNamePattern) Clock(layout string) *LogNamePattern {
	p.segs = append(p.segs, func(meta LogMeta) string { return meta.Time.Format(layout) })
	return p
}

// Timestamp 追加 Unix 时间戳片段。
func (p *LogNamePattern) Timestamp() *LogNamePattern {
	p.segs = append(p.segs, func(meta LogMeta) string { return fmt.Sprintf("%d", meta.Time.Unix()) })
	return p
}

// Char 追加固定字符片段，如 "_"、"-"、"/" 等任意字符串。
func (p *LogNamePattern) Char(s string) *LogNamePattern {
	p.segs = append(p.segs, func(meta LogMeta) string { return s })
	return p
}

// Build 将当前片段组合成 LogName 函数，自动追加 .log 后缀。
func (p *LogNamePattern) Build() LogName {
	segs := make([]nameSegment, len(p.segs))
	copy(segs, p.segs)
	return func(meta LogMeta) string {
		var b strings.Builder
		for _, seg := range segs {
			b.WriteString(seg(meta))
		}
		b.WriteString(".log")
		return b.String()
	}
}

// ============================================================================
// FileWriter — 文件输出
// ============================================================================

// FileWriter 将日志写入文件，支持自动创建目录和动态文件名。
//
// 静态路径：使用 NewFileWriter(path, level)，每次写入同一个文件。
// 动态文件名：使用 NewFileWriterWithLogName(dir, level, logName)，每条日志根据
// LogName 函数动态决定文件名，实现按日期/模块/时间戳等维度自动切分。
type FileWriter struct {
	dir     string   // 日志目录（LogName 模式下使用）
	path    string   // 静态模式下的文件路径
	file    *os.File // 当前打开的文件（静态模式）
	level   Level
	logName LogName // 文件名生成器（动态模式）
	curName string  // 当前文件名缓存，避免重复 open

	rotateSize int64     // 文件大小切割阈值（0 = 不限制）
	written    int64     // 当前文件已写入字节数
	seq        int       // 当前序号，0 = 不追加序号
	formatter  Formatter // nil 时使用默认文本格式
	mu         sync.Mutex
}

// NewFileWriter 创建一个静态文件 Writer（固定路径）。
// path: 日志文件完整路径，父目录不存在时自动创建
// level: 最低输出级别
func NewFileWriter(path string, level Level) (*FileWriter, error) {
	dir := pathDir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("lg: create dir %s: %w", dir, err)
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("lg: open file %s: %w", path, err)
	}
	return &FileWriter{path: path, file: f, level: level}, nil
}

// NewFileWriterWithLogName 创建一个动态文件 Writer。
// dir: 日志文件存放目录
// level: 最低输出级别
// logName: 文件名生成函数，每条日志写入前调用以决定目标文件
//
// 使用 LogNamePattern 链式构建文件名（自动追加 .log 后缀）:
//
//	// pg_2026-08-01.log
//	fw, _ := NewFileWriterWithLogName("logs", LevelInfo,
//	    NewLogNamePattern().Module().Char("_").Date("2006-01-02").Build())
//
//	// pg_2026-08-01_12-30-00.log
//	fw, _ := NewFileWriterWithLogName("logs", LevelInfo,
//	    NewLogNamePattern().Module().Char("_").Date("2006-01-02").Char("_").Clock("15-04-05").Build())
//
//	// 1704067200_pg.log
//	fw, _ := NewFileWriterWithLogName("logs", LevelInfo,
//	    NewLogNamePattern().Timestamp().Char("_").Module().Build())
func NewFileWriterWithLogName(dir string, level Level, logName LogName) (*FileWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("lg: create dir %s: %w", dir, err)
	}
	return &FileWriter{dir: dir, level: level, logName: logName}, nil
}

// pathDir 提取文件路径中的目录部分。
func pathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}

// Level 返回该 Writer 接受的最低日志级别。
func (w *FileWriter) Level() Level { return w.level }

// Write 写入一条日志记录。
// 静态路径模式下，如果超过 rotateSize 阈值，会自动轮转并创建带序号的新文件。
func (w *FileWriter) Write(entry *Entry) error {
	if entry.Level < w.level {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// 动态文件名模式：根据 LogName 决定目标文件
	if w.logName != nil {
		name := w.logName(LogMeta{Module: entry.Module, Time: entry.Time})
		// 序号 > 0 时插入序号：pg_2026-08-01.001.log
		if w.seq > 0 {
			name = insertSeq(name, w.seq)
		}
		if name != w.curName {
			if w.file != nil {
				err := w.file.Close()
				if err != nil {
					return err
				}
				w.written = 0
			}
			fullPath := w.dir + "/" + name
			if d := pathDir(fullPath); d != "" {
				_ = os.MkdirAll(d, 0o755)
			}
			f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return fmt.Errorf("lg: open file %s: %w", fullPath, err)
			}
			w.file = f
			w.curName = name
		}
	}

	line := w.formatLine(entry) + "\n"
	n, err := fmt.Fprint(w.file, line)
	if err != nil {
		return err
	}
	w.written += int64(n)

	// 超过大小阈值：关闭当前文件，递增序号，下次 Write 自动切新文件
	if w.rotateSize > 0 && w.written >= w.rotateSize {
		err := w.file.Close()
		if err != nil {
			return err
		}
		w.file = nil
		w.curName = ""
		w.written = 0
		w.seq++

		// 静态路径模式：创建带序号的新文件
		if w.logName == nil && w.path != "" && w.seq > 0 {
			openPath := insertSeq(w.path, w.seq)
			f, err := os.OpenFile(openPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return fmt.Errorf("lg: open file %s: %w", openPath, err)
			}
			w.file = f
		}
	}

	return nil
}

// insertSeq 在文件名扩展名 .log 前插入序号，如 "pg.log" → "pg.001.log"。
func insertSeq(name string, seq int) string {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return name
	}
	return name[:dot] + fmt.Sprintf(".%03d", seq) + name[dot:]
}

// formatLine 将 Entry 格式化为一行文本。
func (w *FileWriter) formatLine(entry *Entry) string {
	if w.formatter != nil {
		return w.formatter.Format(entry)
	}
	return entry.Format()
}

// SetFormatter 设置格式化器。nil 表示使用默认文本格式。
func (w *FileWriter) SetFormatter(f Formatter) {
	w.formatter = f
}

// SetRotateSize 设置文件大小轮转阈值。
// 超过 size 字节时自动切换新文件，文件名追加 .001 等序号。
// size <= 0 表示关闭大小轮转。
func (w *FileWriter) SetRotateSize(size int64) {
	w.rotateSize = size
}

// Close 关闭 FileWriter，释放底层文件句柄。
func (w *FileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// ============================================================================
// MultiWriter — 多路输出
// ============================================================================

// MultiWriter 将一条日志同时写入多个 Writer。
type MultiWriter struct {
	writers []Writer
}

// NewMultiWriter 创建一个多路 Writer。
func NewMultiWriter(writers ...Writer) *MultiWriter {
	return &MultiWriter{writers: writers}
}

// Level 返回所有子 Writer 中的最低级别。
func (w *MultiWriter) Level() Level {
	minLevel := LevelFatal
	for _, wr := range w.writers {
		if wr.Level() < minLevel {
			minLevel = wr.Level()
		}
	}
	return minLevel
}

// Write 将日志写入所有子 Writer。
// 低于某个子 Writer 级别阈值的日志会被该子 Writer 过滤。
func (w *MultiWriter) Write(entry *Entry) error {
	for _, wr := range w.writers {
		if entry.Level < wr.Level() {
			continue
		}
		_ = wr.Write(entry) // 忽略单个 writer 的错误
	}
	return nil
}

// Close 关闭所有子 Writer。
func (w *MultiWriter) Close() error {
	for _, wr := range w.writers {
		_ = wr.Close()
	}
	return nil
}
