package mux

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Effortful-lion/unibase/mux/internal/msg"
	"github.com/gin-gonic/gin"
)

// ── PerKeyRateLimitMiddleware ───────────────────────────────

func TestPerKeyRateLimit_HTTPByUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var passCount int
	mw := PerKeyRateLimitMiddleware(1, 1) // rate=1, burst=1：每个 user 只允许 1 次

	handler := mw(func(ctx *Context) error {
		passCount++
		return ctx.ReplyOK("ok")
	})

	// 模拟同一 userID 的两次请求：第一次通过，第二次被限流
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		session := msg.NewHTTPRequestSession().WithUserID("user_001")
		muxCtx := newCmdHTTPContext(c, "test.cmd", []byte(`{}`), session)
		handler(muxCtx)
	}

	if passCount != 1 {
		t.Errorf("passCount: got %d, want 1 (second request should be rate limited)", passCount)
	}
}

func TestPerKeyRateLimit_HTTPByIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var passCount int
	mw := PerKeyRateLimitMiddleware(1, 1) // 未认证用户按 IP 限流

	handler := mw(func(ctx *Context) error {
		passCount++
		return ctx.ReplyOK("ok")
	})

	// 同一 IP 的两次请求：第一次通过，第二次被限流
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{}`)))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.RemoteAddr = "192.168.1.100:12345"

		session := msg.NewHTTPRequestSession() // 未设置 userID
		muxCtx := newCmdHTTPContext(c, "test.cmd", []byte(`{}`), session)
		handler(muxCtx)
	}

	if passCount != 1 {
		t.Errorf("passCount: got %d, want 1 (second request should be rate limited by IP)", passCount)
	}
}

func TestPerKeyRateLimit_DifferentKeysIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var passCount int
	mw := PerKeyRateLimitMiddleware(1, 1) // 每个 key 独立限流

	handler := mw(func(ctx *Context) error {
		passCount++
		return ctx.ReplyOK("ok")
	})

	// 两个不同 userID 各发一次请求，都应该通过
	userIDs := []string{"user_A", "user_B"}
	for _, userID := range userIDs {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		session := msg.NewHTTPRequestSession().WithUserID(userID)
		muxCtx := newCmdHTTPContext(c, "test.cmd", []byte(`{}`), session)
		handler(muxCtx)
	}

	if passCount != 2 {
		t.Errorf("passCount: got %d, want 2 (different keys should be independent)", passCount)
	}
}

func TestPerKeyRateLimit_CustomKeyFunc(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var passCount int
	mw := PerKeyRateLimitMiddleware(1, 1, WithKeyFunc(func(ctx *Context) string {
		return "fixed_key" // 所有请求共享同一个限流桶
	}))

	handler := mw(func(ctx *Context) error {
		passCount++
		return ctx.ReplyOK("ok")
	})

	// 两个不同 userID，但使用固定 key，第二次应被限流
	userIDs := []string{"user_A", "user_B"}
	for _, userID := range userIDs {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{}`)))
		c.Request.Header.Set("Content-Type", "application/json")

		session := msg.NewHTTPRequestSession().WithUserID(userID)
		muxCtx := newCmdHTTPContext(c, "test.cmd", []byte(`{}`), session)
		handler(muxCtx)
	}

	if passCount != 1 {
		t.Errorf("passCount: got %d, want 1 (custom fixed key should share limiter)", passCount)
	}
}

func TestPerKeyRateLimit_RateLimitReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mw := PerKeyRateLimitMiddleware(1, 1)

	handler := mw(func(ctx *Context) error {
		return ctx.ReplyOK("ok")
	})

	// 第一次通过
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{}`)))
	c1.Request.Header.Set("Content-Type", "application/json")
	session1 := msg.NewHTTPRequestSession().WithUserID("user_X")
	muxCtx1 := newCmdHTTPContext(c1, "test.cmd", []byte(`{}`), session1)
	handler(muxCtx1)

	// 第二次被限流，应返回 429
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{}`)))
	c2.Request.Header.Set("Content-Type", "application/json")
	session2 := msg.NewHTTPRequestSession().WithUserID("user_X")
	muxCtx2 := newCmdHTTPContext(c2, "test.cmd", []byte(`{}`), session2)
	handler(muxCtx2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request status: got %d, want %d", w2.Code, http.StatusTooManyRequests)
	}
}
