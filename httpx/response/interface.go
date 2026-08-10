package response

import (
	"io"
	"net/http"

	"github.com/yourname/httpx/sse"
)

// ResponseWriter 框架无关的服务端响应写入接口。
// 封装 Gin 没有或薄的能力：SSE、PartialFile 等。
type ResponseWriter interface {
	// JSON 写入 JSON 响应（Gin 原生已有，这里不重复）。
	// JSON(code int, v interface{}) error

	// Stream 写入流式响应。
	Stream(code int, contentType string, r io.Reader) error

	// SSE 写入 Server-Sent Events 流（Gin 原生没有）。
	SSE(it func(w SSEWriter) error) error

	// HTML 写入 HTML 响应。
	HTML(code int, html []byte) error

	// File 写入文件响应。
	File(code int, filepath string) error

	// Header 返回响应头，可在写入前修改。
	Header() http.Header
}

// SSEWriter SSE 事件写入器。
type SSEWriter interface {
	WriteEvent(event sse.Event) error
	Flush()
}
