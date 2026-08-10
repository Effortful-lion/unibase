package sse

import (
	"io"
)

// Writer SSE 写入器，包装 io.Writer 提供便捷的事件写入方法。
type Writer struct {
	w io.Writer
}

// NewWriter 创建 SSE 写入器。
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteEvent 写入一个 SSE 事件。
func (sw *Writer) WriteEvent(event Event) error {
	return event.MarshalTo(sw.w)
}

// Flush 刷新缓冲区（如果底层 Writer 实现了 http.Flusher）。
func (sw *Writer) Flush() {
	if f, ok := sw.w.(interface{ Flush() }); ok {
		f.Flush()
	}
}

// Write 直接写入原始数据。
func (sw *Writer) Write(p []byte) (int, error) {
	return sw.w.Write(p)
}
