package mux_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Effortful-lion/unibase/mux"
	"github.com/Effortful-lion/unibase/mux/internal/websocketx"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ── WebSocket 测试辅助 ────────────────────────────────────────

// startRealServer 在真实 TCP 端口上启动 Engine，返回 wsURL 和清理函数。
// httptest.Server 不支持 WebSocket Hijack，必须使用真实 TCP 连接。
func startRealServer(t *testing.T, engine *mux.Engine) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = engine.Serve(listener)
	}()

	// 等待服务开始监听
	time.Sleep(100 * time.Millisecond)

	wsURL := "ws://" + listener.Addr().String() + "/ws"
	cleanup := func() {
		listener.Close()
		wg.Wait()
	}

	return wsURL, cleanup
}

// dialWebSocket 连接到 WebSocket 服务器并返回连接。
func dialWebSocket(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	ws, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", wsURL, err)
	}
	return ws
}

// ── HTTP 路由测试 ────────────────────────────────────────────

func TestEngine_HTTP_Route(t *testing.T) {
	engine := mux.New()
	engine.HTTP().GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	server := httptest.NewServer(engine)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestEngine_HTTP_Middleware(t *testing.T) {
	engine := mux.New()

	// 注册中间件
	engine.Use(func(c *gin.Context) {
		c.Set("middleware_called", true)
		c.Next()
	})

	engine.HTTP().GET("/api/test", func(c *gin.Context) {
		called, _ := c.Get("middleware_called")
		c.JSON(http.StatusOK, gin.H{"called": called})
	})

	server := httptest.NewServer(engine)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/test")
	if err != nil {
		t.Fatalf("GET /api/test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// ── WebSocket 路由测试 ───────────────────────────────────────

func TestEngine_WebSocket_CmdRouting(t *testing.T) {
	engine := mux.New(mux.WithMaxWebSocketConn(100))

	wsRouter := engine.WS()
	wsRouter.Cmd("chat.echo", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
		var req struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(msg.Body, &req); err != nil {
			return err
		}
		resp := &websocketx.CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"reply":"` + req.Message + `"}`),
		}
		return session.Conn().Write(ctx, resp)
	})

	wsURL, cleanup := startRealServer(t, engine)
	defer cleanup()

	ws := dialWebSocket(t, wsURL)
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	if err := ws.WriteJSON(websocketx.CmdMessage{
		Cmd:  "chat.echo",
		Body: json.RawMessage(`{"message":"hello"}`),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 等待服务端处理并写入响应
	time.Sleep(300 * time.Millisecond)

	var resp websocketx.CmdMessage
	if err := ws.ReadJSON(&resp); err != nil {
		t.Fatalf("read: %v", err)
	}

	if resp.Cmd != "chat.echo" {
		t.Errorf("cmd: got %q, want chat.echo", resp.Cmd)
	}

	code, _ := resp.Meta["code"].(string)
	if code != "10200" {
		t.Errorf("code: got %q, want 10200", code)
	}

	var chatResp struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(resp.Body, &chatResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if chatResp.Reply != "hello" {
		t.Errorf("reply: got %q, want hello", chatResp.Reply)
	}
}

func TestEngine_WebSocket_UnknownCmd(t *testing.T) {
	engine := mux.New(mux.WithMaxWebSocketConn(100))

	wsURL, cleanup := startRealServer(t, engine)
	defer cleanup()

	ws := dialWebSocket(t, wsURL)
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	if err := ws.WriteJSON(websocketx.CmdMessage{
		Cmd:  "unknown.cmd",
		Body: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	var resp websocketx.CmdMessage
	if err := ws.ReadJSON(&resp); err != nil {
		t.Fatalf("read: %v", err)
	}

	code, _ := resp.Meta["code"].(string)
	if code != "10400" {
		t.Errorf("expected 10400, got %q", code)
	}
}

// ── WebSocket JWT Token 注入测试 ──────────────────────────────────

func TestEngine_WebSocket_JWTTokenInjection(t *testing.T) {
	engine := mux.New(mux.WithMaxWebSocketConn(100))

	wsRouter := engine.WS()
	wsRouter.Cmd("auth.check", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
		token, ok := session.Meta()["jwt_token"].(string)
		if !ok {
			return session.Conn().Write(ctx, &websocketx.CmdMessage{
				Cmd:  msg.Cmd,
				Meta: map[string]interface{}{"code": "10500", "message": "jwt_token not found in meta"},
			})
		}
		return session.Conn().Write(ctx, &websocketx.CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"token":"` + token + `"}`),
		})
	})

	// 使用 token 连接
	wsURL, cleanup := startRealServer(t, engine)
	defer cleanup()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL+"?token=test-jwt-123", nil)
	if err != nil {
		t.Fatalf("dial with token: %v", err)
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	if err := ws.WriteJSON(websocketx.CmdMessage{
		Cmd:  "auth.check",
		Body: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp websocketx.CmdMessage
	if err := ws.ReadJSON(&resp); err != nil {
		t.Fatalf("read: %v", err)
	}

	code, _ := resp.Meta["code"].(string)
	if code != "10200" {
		t.Errorf("code: got %q, want 10200", code)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Token != "test-jwt-123" {
		t.Errorf("token: got %q, want test-jwt-123", body.Token)
	}
}

// ── 集成测试：HTTP + WebSocket 同时运行 ─────────────────────

func TestEngine_HTTPAndWebSocket(t *testing.T) {
	engine := mux.New(mux.WithMaxWebSocketConn(100))

	// HTTP 路由
	engine.HTTP().GET("/api/status", func(c *gin.Context) {
		count := engine.WSHub().Count()
		c.JSON(http.StatusOK, gin.H{
			"status":  "running",
			"ws_conn": count,
		})
	})

	// WebSocket 路由
	wsRouter := engine.WS()
	wsRouter.Cmd("ping", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
		resp := &websocketx.CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"reply":"pong"}`),
		}
		return session.Conn().Write(ctx, resp)
	})

	// HTTP 测试（使用 httptest）
	server := httptest.NewServer(engine)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HTTP status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// WebSocket 测试（使用真实 TCP）
	wsURL, cleanup := startRealServer(t, engine)
	defer cleanup()

	ws := dialWebSocket(t, wsURL)
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	if err := ws.WriteJSON(websocketx.CmdMessage{
		Cmd:  "ping",
		Body: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	var wsResp websocketx.CmdMessage
	if err := ws.ReadJSON(&wsResp); err != nil {
		t.Fatalf("read: %v", err)
	}

	var chatResp struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(wsResp.Body, &chatResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if chatResp.Reply != "pong" {
		t.Errorf("reply: got %q, want pong", chatResp.Reply)
	}
}

// ── 优雅关闭测试 ─────────────────────────────────────────────

func TestEngine_Shutdown(t *testing.T) {
	engine := mux.New(mux.WithMaxWebSocketConn(100))
	engine.HTTP().GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	server := httptest.NewServer(engine)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// ── EngineOption 测试 ────────────────────────────────────────

func TestEngine_WithOptions(t *testing.T) {
	engine := mux.New(
		mux.WithReadTimeout(5*time.Second),
		mux.WithWriteTimeout(5*time.Second),
		mux.WithMaxWebSocketConn(10),
	)

	if engine.WSHub() == nil {
		t.Error("WSHub should not be nil")
	}
	if engine.WS() == nil {
		t.Error("WS should not be nil")
	}
	if engine.HTTP() == nil {
		t.Error("HTTP should not be nil")
	}
}

// ── 协议开关测试 ──────────────────────────────────────────────

func TestEngine_Default_BothEnabled(t *testing.T) {
	engine := mux.New()

	if engine.HTTP() == nil {
		t.Error("HTTP engine should not be nil in default mode")
	}
	if engine.WS() == nil {
		t.Error("WS router should not be nil in default mode")
	}
	if engine.WSHub() == nil {
		t.Error("WS hub should not be nil in default mode")
	}
}

func TestEngine_DisableWS_HTTPOnly(t *testing.T) {
	engine := mux.New(mux.DisableWS())

	if engine.HTTP() == nil {
		t.Error("HTTP engine should not be nil in HTTP-only mode")
	}
	if engine.WS() != nil {
		t.Error("WS router should be nil in HTTP-only mode")
	}
	if engine.WSHub() != nil {
		t.Error("WS hub should be nil in HTTP-only mode")
	}

	// 验证 HTTP 路由正常工作
	engine.HTTP().GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	server := httptest.NewServer(engine)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestEngine_DisableHTTP_WSOnly(t *testing.T) {
	engine := mux.New(mux.DisableHTTP())

	if engine.WS() == nil {
		t.Error("WS router should not be nil in WS-only mode")
	}
	if engine.WSHub() == nil {
		t.Error("WS hub should not be nil in WS-only mode")
	}

	// Serve() 之前 HTTP() 为 nil（transport 在启动时才创建）
	if engine.HTTP() != nil {
		t.Error("HTTP engine should be nil before Serve() in WS-only mode")
	}

	// 注册 WS Cmd
	wsRouter := engine.WS()
	wsRouter.Cmd("ping", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
		return session.Conn().Write(ctx, &websocketx.CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"reply":"pong"}`),
		})
	})

	// 启动服务
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go engine.Serve(listener)
	defer listener.Close()

	time.Sleep(100 * time.Millisecond)

	// 启动后 HTTP() 返回承载 Upgrade 的最小引擎
	if engine.HTTP() == nil {
		t.Error("HTTP engine should not be nil after Serve() in WS-only mode")
	}

	// 验证 WebSocket 连接
	wsURL := "ws://" + listener.Addr().String() + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	if err := ws.WriteJSON(websocketx.CmdMessage{
		Cmd:  "ping",
		Body: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	var resp websocketx.CmdMessage
	if err := ws.ReadJSON(&resp); err != nil {
		t.Fatalf("read: %v", err)
	}

	code, _ := resp.Meta["code"].(string)
	if code != "10200" {
		t.Errorf("code: got %q, want 10200", code)
	}
}

// ── 集成 Demo：Chat Server ────────────────────────────────────
// TestChatDemo 演示如何使用 mux 构建一个完整的 Chat 服务。
// 包含 HTTP API（健康检查、房间列表）和 WebSocket（加入房间、发送消息、离开房间）。
//
// 运行方式：
//
//	go test -v -run TestChatDemo -timeout 30s
//
// 验证点：
//   - HTTP GET /api/health 返回 200
//   - HTTP GET /api/rooms 返回房间列表
//   - WebSocket chat.join 加入房间
//   - WebSocket chat.message 发送消息并收到广播
//   - WebSocket chat.leave 离开房间
//   - WebSocket ping/pong 心跳检测
func TestChatDemo(t *testing.T) {
	engine := mux.New(
		mux.WithMaxWebSocketConn(10000),
		mux.WithReadTimeout(10*time.Second),
		mux.WithWriteTimeout(10*time.Second),
	)

	// ── 全局状态 ───────────────────────────────────────────────
	userRegistry := &sync.Map{}

	// ── HTTP 路由 ──────────────────────────────────────────────
	httpEngine := engine.HTTP()
	httpEngine.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	httpEngine.GET("/api/rooms", func(c *gin.Context) {
		roomMap := map[string][]string{}
		userRegistry.Range(func(key, value any) bool {
			roomMap[value.(string)] = append(roomMap[value.(string)], key.(string))
			return true
		})
		c.JSON(http.StatusOK, roomMap)
	})

	// ── WebSocket 路由 ─────────────────────────────────────────
	wsRouter := engine.WS()
	wsRouter.Use(websocketx.RecoverMiddleware)

	wsRouter.Cmd("chat.join", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
		var req struct {
			Room string `json:"room"`
		}
		msg.Bind(&req)
		session.JoinRoom(req.Room)
		userRegistry.Store(session.ID(), req.Room)
		return session.Conn().Write(ctx, &websocketx.CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"room":"` + req.Room + `"}`),
		})
	})

	wsRouter.Cmd("chat.message", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
		var req struct {
			Room    string `json:"room"`
			Message string `json:"message"`
		}
		msg.Bind(&req)
		broadcast := &websocketx.CmdMessage{
			Cmd:  "chat.broadcast",
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"room":"` + req.Room + `","message":"` + req.Message + `","from":"` + session.ID() + `"}`),
		}
		_ = engine.WSHub().BroadcastToRoom(ctx, req.Room, broadcast, session.ID())
		return session.Conn().Write(ctx, &websocketx.CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"room":"` + req.Room + `","message":"` + req.Message + `"}`),
		})
	})

	wsRouter.Cmd("chat.leave", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
		var req struct {
			Room string `json:"room"`
		}
		msg.Bind(&req)
		session.LeaveRoom(req.Room)
		userRegistry.Delete(session.ID())
		return session.Conn().Write(ctx, &websocketx.CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"room":"` + req.Room + `"}`),
		})
	})

	wsRouter.Cmd("ping", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
		return session.Conn().Write(ctx, &websocketx.CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"time":` + strconv.FormatInt(time.Now().Unix(), 10) + `}`),
		})
	})

	// ── 启动服务 ───────────────────────────────────────────────
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	go engine.Serve(listener)
	defer listener.Close()

	time.Sleep(100 * time.Millisecond)

	// ── 验证 HTTP ──────────────────────────────────────────────
	resp, _ := http.Get("http://" + listener.Addr().String() + "/api/health")
	if resp != nil {
		resp.Body.Close()
		fmt.Printf("HTTP /api/health: %d\n", resp.StatusCode)
	}

	// ── 验证 WebSocket ─────────────────────────────────────────
	wsURL := "ws://" + listener.Addr().String() + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		fmt.Printf("WebSocket dial failed: %v\n", err)
		return
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	// 发送 ping
	ws.WriteJSON(websocketx.CmdMessage{Cmd: "ping", Body: json.RawMessage(`{}`)})
	time.Sleep(200 * time.Millisecond)

	var pingResp websocketx.CmdMessage
	if err := ws.ReadJSON(&pingResp); err == nil {
		fmt.Printf("WebSocket ping: code=%s\n", pingResp.Meta["code"])
	}

	fmt.Println("Chat server demo completed!")
}
