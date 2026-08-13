package mux

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Effortful-lion/unibase/mux/internal/msg"
	"github.com/Effortful-lion/unibase/mux/internal/websocketx"
	"github.com/gin-gonic/gin"
)

// ── Context.Set/Get/MustGet ──────────────────────────────────

func TestContext_SetGet(t *testing.T) {
	ctx := &Context{holder: make(map[string]any)}
	ctx.Set("key", "value")

	v, ok := ctx.Get("key")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if v != "value" {
		t.Errorf("got %v, want value", v)
	}
}

func TestContext_MustGet_Existing(t *testing.T) {
	ctx := &Context{holder: map[string]any{"k": 42}}
	v := ctx.MustGet("k")
	if v != 42 {
		t.Errorf("got %v, want 42", v)
	}
}

func TestContext_MustGet_Missing(t *testing.T) {
	ctx := &Context{holder: make(map[string]any)}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	ctx.MustGet("missing")
}

// ── Context.Bind ─────────────────────────────────────────────

func TestContext_Bind_CmdHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{"name":"test"}`)))
	c.Request.Header.Set("Content-Type", "application/json")

	session := msg.NewHTTPRequestSession()
	ctx := newCmdHTTPContext(c, "test.cmd", []byte(`{"name":"test"}`), session)

	var req struct{ Name string }
	if err := ctx.Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Name != "test" {
		t.Errorf("got %q, want test", req.Name)
	}
}

func TestContext_Bind_CmdWS(t *testing.T) {
	wsSession := msg.NewWSSession(&websocketx.Session{})
	ctx := newCmdWSContext(context.Background(), "test.cmd", []byte(`{"count":10}`), wsSession)

	var req struct{ Count int }
	if err := ctx.Bind(&req); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if req.Count != 10 {
		t.Errorf("got %d, want 10", req.Count)
	}
}

func TestContext_Bind_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{invalid}`)))
	c.Request.Header.Set("Content-Type", "application/json")

	session := msg.NewHTTPRequestSession()
	ctx := newCmdHTTPContext(c, "test.cmd", []byte(`{invalid}`), session)

	var req struct{ Name string }
	if err := ctx.Bind(&req); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ── Context.Reply ────────────────────────────────────────────

func TestContext_ReplyREST(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/", nil)

	session := msg.NewHTTPRequestSession()
	ctx := newRESTContext(c, session)

	if err := ctx.ReplyOK(map[string]string{"status": "ok"}); err != nil {
		t.Fatalf("ReplyOK: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status: got %q, want ok", resp["status"])
	}
}

func TestContext_ReplyError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/", nil)

	session := msg.NewHTTPRequestSession()
	ctx := newRESTContext(c, session)

	if err := ctx.ReplyError(http.StatusBadRequest, "bad request"); err != nil {
		t.Fatalf("ReplyError: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

// ── Context.Protocol ─────────────────────────────────────────

func TestContext_Protocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("GET", "/", nil)
	session := msg.NewHTTPRequestSession()

	restCtx := newRESTContext(c, session)
	if restCtx.Protocol() != ProtocolHTTP {
		t.Errorf("REST Protocol: got %v, want ProtocolHTTP", restCtx.Protocol())
	}
}

// ── Context.Source ───────────────────────────────────────────

func TestContext_Source(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("GET", "/", nil)
	session := msg.NewHTTPRequestSession()
	ctx := newRESTContext(c, session)

	if ctx.Source() == nil {
		t.Fatal("Source() returned nil")
	}
}

// ── Pipeline Cmd routing ─────────────────────────────────────

func TestPipeline_CmdRouting(t *testing.T) {
	var called bool
	p := NewPipeline(nil)
	p.Cmd("test.cmd", func(ctx *Context) error {
		called = true
		return ctx.ReplyOK("ok")
	})

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{"cmd":"test.cmd"}`)))
	c.Request.Header.Set("Content-Type", "application/json")

	session := msg.NewHTTPRequestSession()
	muxCtx := newCmdHTTPContext(c, "test.cmd", []byte(`"payload"`), session)
	p.executeCmd(muxCtx)

	if !called {
		t.Error("handler was not called")
	}
}

func TestPipeline_CmdNotFound(t *testing.T) {
	p := NewPipeline(nil)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{"cmd":"missing.cmd"}`)))
	c.Request.Header.Set("Content-Type", "application/json")

	session := msg.NewHTTPRequestSession()
	muxCtx := newCmdHTTPContext(c, "missing.cmd", []byte{}, session)
	p.executeCmd(muxCtx)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

// ── Pipeline middleware chain ─────────────────────────────────

func TestPipeline_MiddlewareChain(t *testing.T) {
	var order []string

	p := NewPipeline(nil)
	p.Use(func(next Handler) Handler {
		return func(ctx *Context) error {
			order = append(order, "mw1")
			return next(ctx)
		}
	})
	p.Use(func(next Handler) Handler {
		return func(ctx *Context) error {
			order = append(order, "mw2")
			return next(ctx)
		}
	})
	p.Cmd("test", func(ctx *Context) error {
		order = append(order, "handler")
		return ctx.ReplyOK("ok")
	})

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{"cmd":"test"}`)))
	c.Request.Header.Set("Content-Type", "application/json")

	session := msg.NewHTTPRequestSession()
	muxCtx := newCmdHTTPContext(c, "test", []byte{}, session)
	p.executeCmd(muxCtx)

	expected := []string{"mw1", "mw2", "handler"}
	if len(order) != len(expected) {
		t.Fatalf("order: got %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d]: got %q, want %q", i, order[i], v)
		}
	}
}

// ── Pipeline prefix middleware ────────────────────────────────

func TestPipeline_UsePrefix(t *testing.T) {
	var matched bool
	p := NewPipeline(nil)
	p.UsePrefix("file.*", func(next Handler) Handler {
		return func(ctx *Context) error {
			matched = true
			return next(ctx)
		}
	})
	p.Cmd("file.upload", func(ctx *Context) error {
		return ctx.ReplyOK("ok")
	})

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", bytes.NewReader([]byte(`{"cmd":"file.upload"}`)))
	c.Request.Header.Set("Content-Type", "application/json")

	session := msg.NewHTTPRequestSession()
	muxCtx := newCmdHTTPContext(c, "file.upload", []byte{}, session)
	p.executeCmd(muxCtx)

	if !matched {
		t.Error("prefix middleware was not matched")
	}
}

// ── Session implementations ───────────────────────────────────

func TestHTTPRequestSession(t *testing.T) {
	s := msg.NewHTTPRequestSession()
	if s.ID() == "" {
		t.Error("expected non-empty ID")
	}

	s.SetState("key", "value")
	v, ok := s.GetState("key")
	if !ok || v != "value" {
		t.Errorf("GetState: got %v, ok=%v, want value, true", v, ok)
	}

	s.WithUserID("user_123")
	if s.UserID() != "user_123" {
		t.Errorf("UserID: got %s, want user_123", s.UserID())
	}
}
