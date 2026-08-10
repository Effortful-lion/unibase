package response

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yourname/httpx/sse"
)

// GinResponseWriter 基于 Gin 的 ResponseWriter 实现。
// 封装 Gin 没有的增强能力：SSE、Stream 等。
type GinResponseWriter struct {
	c *gin.Context
}

// FromGin 从 gin.Context 创建 ResponseWriter。
func FromGin(c *gin.Context) ResponseWriter {
	return &GinResponseWriter{c: c}
}

func (g *GinResponseWriter) Stream(code int, contentType string, r io.Reader) error {
	g.c.Header("Content-Type", contentType)
	g.c.Status(code)
	g.c.Stream(func(w io.Writer) bool {
		_, err := io.Copy(w, r)
		return err == nil
	})
	return nil
}

func (g *GinResponseWriter) SSE(it func(w SSEWriter) error) error {
	g.c.Header("Content-Type", "text/event-stream")
	g.c.Header("Cache-Control", "no-cache")
	g.c.Header("Connection", "keep-alive")
	g.c.Status(http.StatusOK)
	g.c.Stream(func(sw io.Writer) bool {
		err := it(&sseWriterAdapter{Writer: sw})
		return err == nil
	})
	return nil
}

func (g *GinResponseWriter) HTML(code int, html []byte) error {
	g.c.Data(code, "text/html; charset=utf-8", html)
	return nil
}

func (g *GinResponseWriter) File(code int, filepath string) error {
	g.c.File(filepath)
	return nil
}

func (g *GinResponseWriter) Header() http.Header {
	return g.c.Writer.Header()
}

// sseWriterAdapter 将 io.Writer 适配为 SSEWriter。
type sseWriterAdapter struct {
	io.Writer
}

func (a *sseWriterAdapter) WriteEvent(event sse.Event) error {
	return event.MarshalTo(a.Writer)
}

func (a *sseWriterAdapter) Flush() {
	if f, ok := a.Writer.(interface{ Flush() }); ok {
		f.Flush()
	}
}
