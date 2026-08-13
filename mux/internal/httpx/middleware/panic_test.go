package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/gin-gonic/gin"
)

// captureLogs 使用一个自定义 Writer 捕获日志输出。
type captureLogs struct {
	buf *bytes.Buffer
}

func newCaptureLogs() *captureLogs {
	return &captureLogs{buf: &bytes.Buffer{}}
}

func (c *captureLogs) Write(entry *logx.Entry) error {
	c.buf.WriteString(entry.Format() + "\n")
	return nil
}

func (c *captureLogs) String() string {
	return c.buf.String()
}

func (c *captureLogs) Level() logx.Level {
	return logx.LevelDebug
}

// Close 满足 logx.Writer 接口。
func (c *captureLogs) Close() error {
	return nil
}

// setupTestContext 创建测试用的 gin 上下文。
func setupTestContext(method, path string, body string) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(method, path, strings.NewReader(body))
	return w, c
}

// ── Panic 中间件 ─────────────────────────────────────────────────

func TestPanic_RecoverFromPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w, c := setupTestContext("GET", "/test", "")

	capturer := newCaptureLogs()
	logger := logx.New(capturer)

	r := gin.New()
	r.Use(Panic(WithPanicLogger(logger)))
	r.GET("/test", func(c *gin.Context) {
		panic("intentional panic")
	})
	r.ServeHTTP(c.Writer, c.Request)

	// 应返回 500
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}

	// 应记录 panic 日志
	logs := capturer.String()
	if !strings.Contains(logs, "panic recovered") {
		t.Errorf("expected panic log, got: %s", logs)
	}
	if !strings.Contains(logs, "intentional panic") {
		t.Errorf("expected panic value in log, got: %s", logs)
	}

	// 应返回统一错误响应格式
	var resp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("parse response error: %v", err)
	}
	if resp.Code != "10500" {
		t.Errorf("response code: got %q, want %q", resp.Code, "10500")
	}
	if resp.Data != nil {
		t.Errorf("response data: got %v, want nil", resp.Data)
	}
}

func TestPanic_NormalRequestPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w, c := setupTestContext("GET", "/test", "")

	r := gin.New()
	r.Use(Panic())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.ServeHTTP(c.Writer, c.Request)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestPanic_PanicInMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w, c := setupTestContext("GET", "/test", "")

	capturer := newCaptureLogs()
	logger := logx.New(capturer)

	// Panic 中间件必须在可能 panic 的中间件之前注册
	r := gin.New()
	r.Use(Panic(WithPanicLogger(logger)))
	r.Use(func(c *gin.Context) {
		panic("panic in middleware")
	})
	r.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.ServeHTTP(c.Writer, c.Request)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestPanic_DefaultLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w, c := setupTestContext("GET", "/test", "")

	r := gin.New()
	r.Use(Panic()) // 不注入 logger，应使用默认
	r.GET("/test", func(c *gin.Context) {
		panic("test panic")
	})
	r.ServeHTTP(c.Writer, c.Request)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
