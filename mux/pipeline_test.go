package mux_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux"
)

// ── 测试辅助 ──────────────────────────────────────────────────

func newTestEngine(t *testing.T) (*mux.Engine, *httptest.Server) {
	t.Helper()
	engine := mux.New(mux.WithHTTPAddr(":0"))
	server := httptest.NewServer(engine)
	return engine, server
}

// ── 1. REST 路由匹配 ──────────────────────────────────────────

func TestPipeline_RESTRoute(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)
	var captured string
	pipeline.GET("/api/hello", func(ctx *mux.Context) error {
		captured = ctx.Cmd()
		return ctx.ReplyOK(map[string]string{"msg": "hi"})
	})

	engine.UsePipeline(pipeline)

	resp, err := http.Get(server.URL + "/api/hello")
	if err != nil {
		t.Fatalf("GET /api/hello: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if captured != "/api/hello" {
		t.Errorf("cmd: got %q, want %q", captured, "/api/hello")
	}
}

// ── 2. Cmd 路由匹配 ──────────────────────────────────────────

func TestPipeline_CmdRoute(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)
	var captured string
	pipeline.Cmd("user.list", func(ctx *mux.Context) error {
		captured = ctx.Cmd()
		return ctx.ReplyOK(map[string]string{"user": "alice"})
	})

	engine.UsePipeline(pipeline)

	body := `{"cmd":"user.list","body":{}}`
	resp, err := http.Post(server.URL+"/v1/cmd", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/cmd: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if captured != "user.list" {
		t.Errorf("cmd: got %q, want %q", captured, "user.list")
	}
}

// ── 3. 全局中间件链执行 ──────────────────────────────────────

func TestPipeline_GlobalMiddleware(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)

	var order []string
	pipeline.Use(
		func(next mux.Handler) mux.Handler {
			return func(ctx *mux.Context) error {
				order = append(order, "mw1_before")
				err := next(ctx)
				order = append(order, "mw1_after")
				return err
			}
		},
		func(next mux.Handler) mux.Handler {
			return func(ctx *mux.Context) error {
				order = append(order, "mw2_before")
				err := next(ctx)
				order = append(order, "mw2_after")
				return err
			}
		},
	)

	pipeline.GET("/api/test", func(ctx *mux.Context) error {
		order = append(order, "handler")
		return ctx.ReplyOK(nil)
	})

	engine.UsePipeline(pipeline)

	resp, err := http.Get(server.URL + "/api/test")
	if err != nil {
		t.Fatalf("GET /api/test: %v", err)
	}
	defer resp.Body.Close()

	expected := []string{"mw1_before", "mw2_before", "handler", "mw2_after", "mw1_after"}
	if len(order) != len(expected) {
		t.Fatalf("order: got %v, want %v", order, expected)
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Errorf("order[%d]: got %q, want %q", i, order[i], expected[i])
		}
	}
}

// ── 4. 命令级中间件（UsePrefix）───────────────────────────────

func TestPipeline_PrefixMiddleware(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)

	pipeline.UsePrefix("file.*", func(next mux.Handler) mux.Handler {
		return func(ctx *mux.Context) error {
			ctx.Set("prefix_mw_called", true)
			return next(ctx)
		}
	})

	pipeline.Cmd("file.upload", func(ctx *mux.Context) error {
		called, _ := ctx.Get("prefix_mw_called")
		if called == nil {
			return ctx.ReplyError(http.StatusInternalServerError, "prefix middleware not called")
		}
		return ctx.ReplyOK(map[string]string{"status": "uploaded"})
	})

	pipeline.Cmd("user.create", func(ctx *mux.Context) error {
		_, _ = ctx.Get("prefix_mw_called")
		return ctx.ReplyOK(map[string]string{"status": "created"})
	})

	engine.UsePipeline(pipeline)

	// file.upload → 前缀匹配
	resp, err := http.Post(server.URL+"/v1/cmd", "application/json", strings.NewReader(`{"cmd":"file.upload","body":{}}`))
	if err != nil {
		t.Fatalf("POST /v1/cmd: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("file.upload status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// user.create → 前缀不匹配
	resp2, err := http.Post(server.URL+"/v1/cmd", "application/json", strings.NewReader(`{"cmd":"user.create","body":{}}`))
	if err != nil {
		t.Fatalf("POST /v1/cmd: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("user.create status: got %d, want %d", resp2.StatusCode, http.StatusOK)
	}
}

// ── 5. Handler 同时注册在 REST 和 Cmd ────────────────────────

func TestPipeline_SharedHandler(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)

	callCount := 0
	sharedHandler := func(ctx *mux.Context) error {
		callCount++
		return ctx.ReplyOK(map[string]int{"count": callCount})
	}

	pipeline.GET("/api/users", sharedHandler)
	pipeline.Cmd("user.list", sharedHandler)

	engine.UsePipeline(pipeline)

	// REST
	resp, err := http.Get(server.URL + "/api/users")
	if err != nil {
		t.Fatalf("GET /api/users: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("REST status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Cmd
	resp2, err := http.Post(server.URL+"/v1/cmd", "application/json", strings.NewReader(`{"cmd":"user.list","body":{}}`))
	if err != nil {
		t.Fatalf("POST /v1/cmd: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Cmd status: got %d, want %d", resp2.StatusCode, http.StatusOK)
	}

	if callCount != 2 {
		t.Errorf("callCount: got %d, want 2", callCount)
	}
}

// ── 6. Context.Bind 在不同 mode 下的行为 ─────────────────────

func TestContext_BindREST(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)

	var received struct {
		Name string `json:"name"`
	}

	pipeline.POST("/api/users", func(ctx *mux.Context) error {
		if err := ctx.Bind(&received); err != nil {
			return ctx.ReplyError(http.StatusBadRequest, err.Error())
		}
		return ctx.ReplyOK(received)
	})

	engine.UsePipeline(pipeline)

	resp, err := http.Post(server.URL+"/api/users", "application/json", strings.NewReader(`{"name":"alice"}`))
	if err != nil {
		t.Fatalf("POST /api/users: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["name"] != "alice" {
		t.Errorf("name: got %q, want %q", result["name"], "alice")
	}
}

func TestContext_BindCmd(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)

	var received struct {
		Name string `json:"name"`
	}

	pipeline.Cmd("user.create", func(ctx *mux.Context) error {
		if err := ctx.Bind(&received); err != nil {
			return ctx.ReplyError(http.StatusBadRequest, err.Error())
		}
		return ctx.ReplyOK(received)
	})

	engine.UsePipeline(pipeline)

	body := `{"cmd":"user.create","body":{"name":"bob"}}`
	resp, err := http.Post(server.URL+"/v1/cmd", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/cmd: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	bodyMap, ok := result["body"].(map[string]any)
	if !ok {
		t.Fatalf("body: got %T", result["body"])
	}
	if bodyMap["name"] != "bob" {
		t.Errorf("name: got %v, want %q", bodyMap["name"], "bob")
	}
}

// ── 7. Context.Reply 在不同 mode 下的输出 ────────────────────

func TestContext_ReplyREST(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)

	pipeline.GET("/api/test", func(ctx *mux.Context) error {
		return ctx.ReplyOK(map[string]string{"mode": "rest"})
	})

	engine.UsePipeline(pipeline)

	resp, err := http.Get(server.URL + "/api/test")
	if err != nil {
		t.Fatalf("GET /api/test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["mode"] != "rest" {
		t.Errorf("mode: got %q, want %q", result["mode"], "rest")
	}
}

func TestContext_ReplyCmdHTTP(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)

	pipeline.Cmd("user.info", func(ctx *mux.Context) error {
		return ctx.ReplyOK(map[string]string{"user": "charlie"})
	})

	engine.UsePipeline(pipeline)

	body := `{"cmd":"user.info","body":{}}`
	resp, err := http.Post(server.URL+"/v1/cmd", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/cmd: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["cmd"] != "user.info" {
		t.Errorf("cmd: got %v", result["cmd"])
	}
	head, ok := result["head"].(map[string]any)
	if !ok {
		t.Fatal("head missing")
	}
	code, ok := head["code"].(float64)
	if !ok || int(code) != http.StatusOK {
		t.Errorf("code: got %v", head["code"])
	}
	bodyResult, ok := result["body"].(map[string]any)
	if !ok {
		t.Fatalf("body type: got %T", result["body"])
	}
	if bodyResult["user"] != "charlie" {
		t.Errorf("user: got %v, want charlie", bodyResult["user"])
	}
}

// ── 8. LogRoutes 输出路由信息 ──────────────────────────────────

func TestPipeline_LogRoutes(t *testing.T) {
	engine, server := newTestEngine(t)
	defer server.Close()

	pipeline := mux.NewPipeline(engine)

	pipeline.GET("/api/health", func(ctx *mux.Context) error {
		return ctx.ReplyOK(nil)
	})
	pipeline.POST("/api/users", func(ctx *mux.Context) error {
		return ctx.ReplyOK(nil)
	})
	pipeline.Cmd("user.list", func(ctx *mux.Context) error {
		return ctx.ReplyOK(nil)
	})
	pipeline.Cmd("chat.message", func(ctx *mux.Context) error {
		return ctx.ReplyOK(nil)
	})

	engine.UsePipeline(pipeline)

	// 使用内存 writer 捕获日志输出
	var buf strings.Builder
	writer := logx.NewConsoleWriter(&buf, logx.LevelInfo)
	logger := logx.New(writer).Module("test")

	pipeline.LogRoutes(logger)

	output := buf.String()
	if !strings.Contains(output, "GET /api/health") {
		t.Errorf("LogRoutes output missing GET /api/health:\n%s", output)
	}
	if !strings.Contains(output, "POST /api/users") {
		t.Errorf("LogRoutes output missing POST /api/users:\n%s", output)
	}
	if !strings.Contains(output, "POST /v1/cmd") {
		t.Errorf("LogRoutes output missing POST /v1/cmd:\n%s", output)
	}
	if !strings.Contains(output, "user.list") {
		t.Errorf("LogRoutes output missing user.list:\n%s", output)
	}
	if !strings.Contains(output, "chat.message") {
		t.Errorf("LogRoutes output missing chat.message:\n%s", output)
	}
}
