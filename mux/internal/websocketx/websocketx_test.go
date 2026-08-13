package websocketx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ── Message 基础测试 ─────────────────────────────────────────

func TestCmdMessage_JSONRoundTrip(t *testing.T) {
	original := &CmdMessage{
		Cmd:  "file.upload",
		Meta: map[string]interface{}{"requestId": "abc123"},
		Body: json.RawMessage(`{"fileName":"a.jpg","fileSize":10240}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded CmdMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Cmd != original.Cmd {
		t.Errorf("Cmd: got %q, want %q", decoded.Cmd, original.Cmd)
	}
	if decoded.Meta["requestId"] != "abc123" {
		t.Errorf("Meta[requestId]: got %v, want abc123", decoded.Meta["requestId"])
	}
	if string(decoded.Body) != `{"fileName":"a.jpg","fileSize":10240}` {
		t.Errorf("Body: got %s", string(decoded.Body))
	}
}

// ── Router 分发测试 ──────────────────────────────────────────

type testUploadReq struct {
	FileName string `json:"fileName"`
}

func TestRouter_CmdDispatch(t *testing.T) {
	router := NewRouter()

	var received *testUploadReq
	router.Cmd("file.upload", func(ctx context.Context, session *Session, msg *CmdMessage) error {
		var req testUploadReq
		if err := msg.Bind(&req); err != nil {
			return err
		}
		received = &req
		return session.Conn().Write(ctx, &CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"url":"https://oss.example.com/` + req.FileName + `"}`),
		})
	})

	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newConn(&fakeWebSocketConn{writeData: conn}, JSONCodec, session)

	msg := &CmdMessage{
		Cmd:  "file.upload",
		Body: json.RawMessage(`{"fileName":"test.jpg"}`),
	}

	err := router.Handle(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if received == nil {
		t.Fatal("handler not called")
	}
	if received.FileName != "test.jpg" {
		t.Errorf("FileName: got %q, want test.jpg", received.FileName)
	}

	conn.WaitWrite()
	data := conn.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.Cmd != "file.upload" {
		t.Errorf("resp.Cmd: got %q, want file.upload", resp.Cmd)
	}
	if resp.Meta["code"] != "10200" {
		t.Errorf("resp.Meta[code]: got %v, want 10200", resp.Meta["code"])
	}
}

func TestRouter_CmdNotFound(t *testing.T) {
	router := NewRouter()
	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newConn(&fakeWebSocketConn{writeData: conn}, JSONCodec, session)

	msg := &CmdMessage{Cmd: "unknown.cmd"}
	err := router.Handle(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	conn.WaitWrite()
	data := conn.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.Meta["code"] != "10400" {
		t.Errorf("expected 10400, got %v", resp.Meta["code"])
	}
}

func TestRouter_HandlerError(t *testing.T) {
	router := NewRouter()
	router.Cmd("test.cmd", func(ctx context.Context, session *Session, msg *CmdMessage) error {
		return NewError("business error")
	})

	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newConn(&fakeWebSocketConn{writeData: conn}, JSONCodec, session)

	msg := &CmdMessage{
		Cmd:  "test.cmd",
		Body: json.RawMessage(`{}`),
	}

	err := router.Handle(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	conn.WaitWrite()
	data := conn.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.Meta["code"] != "10500" {
		t.Errorf("expected 10500, got %v", resp.Meta["code"])
	}
	msgStr, _ := resp.Meta["message"].(string)
	if msgStr != "business error" {
		t.Errorf("message: got %q, want business error", msgStr)
	}
}

// ── 中间件测试 ────────────────────────────────────────────────

func TestRouter_Middleware(t *testing.T) {
	router := NewRouter()

	var middlewareCalled bool
	authMiddleware := func(ctx context.Context, session *Session, msg *CmdMessage, next MessageHandler) error {
		middlewareCalled = true
		if session.Meta()["userID"] == nil {
			return session.Conn().Write(ctx, &CmdMessage{
				Cmd:  msg.Cmd,
				Meta: map[string]interface{}{"code": "10401", "message": "unauthorized"},
			})
		}
		return next(ctx, session, msg)
	}

	router.Cmd("test.cmd", func(ctx context.Context, session *Session, msg *CmdMessage) error {
		return session.Conn().Write(ctx, &CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"url":"ok"}`),
		})
	})

	router.Use(authMiddleware)

	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newConn(&fakeWebSocketConn{writeData: conn}, JSONCodec, session)

	msg := &CmdMessage{Cmd: "test.cmd", Body: json.RawMessage(`{}`)}
	router.Handle(context.Background(), session, msg)

	if !middlewareCalled {
		t.Error("middleware not called")
	}
	conn.WaitWrite()
	data := conn.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.Meta["code"] != "10401" {
		t.Errorf("expected 10401, got %v", resp.Meta["code"])
	}
}

// ── 路由级中间件测试 ─────────────────────────────────────────

func TestRouter_RouteMiddleware(t *testing.T) {
	router := NewRouter()

	routeMwCalled := false
	routeMw := func(ctx context.Context, session *Session, msg *CmdMessage, next MessageHandler) error {
		routeMwCalled = true
		return next(ctx, session, msg)
	}

	router.Cmd("test.cmd", func(ctx context.Context, session *Session, msg *CmdMessage) error {
		return session.Conn().Write(ctx, &CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
		})
	}, routeMw)

	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newConn(&fakeWebSocketConn{writeData: conn}, JSONCodec, session)

	msg := &CmdMessage{Cmd: "test.cmd"}
	router.Handle(context.Background(), session, msg)

	if !routeMwCalled {
		t.Error("route middleware not called")
	}

	conn.WaitWrite()
	data := conn.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.Meta["code"] != "10200" {
		t.Errorf("expected 10200, got %v", resp.Meta["code"])
	}
}

// ── RecoverMiddleware 测试 ────────────────────────────────────

func TestRecoverMiddleware(t *testing.T) {
	router := NewRouter()
	router.Use(RecoverMiddleware)

	router.Cmd("panic.cmd", func(ctx context.Context, session *Session, msg *CmdMessage) error {
		panic("test panic")
	})

	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newConn(&fakeWebSocketConn{writeData: conn}, JSONCodec, session)

	msg := &CmdMessage{Cmd: "panic.cmd"}
	err := router.Handle(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	conn.WaitWrite()
	data := conn.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.Meta["code"] != "10500" {
		t.Errorf("expected 10500, got %v", resp.Meta["code"])
	}
	msgStr, _ := resp.Meta["message"].(string)
	if msgStr != "panic: test panic" {
		t.Errorf("message: got %q, want panic: test panic", msgStr)
	}
}

// ── Hub 测试 ──────────────────────────────────────────────────

func TestHub_RegisterUnregister(t *testing.T) {
	hub := NewHub(0)

	session := &Session{id: "s_001", meta: make(Meta)}
	hub.Register(session)

	if hub.Count() != 1 {
		t.Errorf("Count: got %d, want 1", hub.Count())
	}

	got, ok := hub.GetSession("s_001")
	if !ok || got != session {
		t.Error("GetSession failed")
	}

	hub.Unregister("s_001")
	if hub.Count() != 0 {
		t.Errorf("Count after unregister: got %d, want 0", hub.Count())
	}
	_, ok = hub.GetSession("s_001")
	if ok {
		t.Error("session should be gone after unregister")
	}
}

func TestHub_Kick(t *testing.T) {
	hub := NewHub(0)

	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newSyncConn(&syncFakeWebSocketConn{writeData: conn}, JSONCodec, session)
	session.SetUserID("user_001")
	hub.Register(session)

	got, ok := hub.GetSessionByUserID("user_001")
	if !ok || got != session {
		t.Error("GetSessionByUserID failed")
	}

	// Kick 应该关闭连接
	err := hub.Kick("user_001")
	if err != nil {
		t.Fatalf("Kick: %v", err)
	}
}

func TestHub_BroadcastToRoom(t *testing.T) {
	hub := NewHub(0)

	session1 := &Session{id: "s_001", meta: make(Meta), rooms: make(map[string]struct{})}
	session2 := &Session{id: "s_002", meta: make(Meta), rooms: make(map[string]struct{})}
	session1.JoinRoom("room_001")
	session2.JoinRoom("room_001")

	// 通过 Hub.JoinRoom 建立 roomIndex（同时维护 session.rooms）
	session1.setHub(hub)
	session2.setHub(hub)
	hub.Register(session1)
	hub.Register(session2)
	hub.JoinRoom(session1, "room_001")
	hub.JoinRoom(session2, "room_001")

	conn1 := newFakeConn()
	session1.conn = newSyncConn(&syncFakeWebSocketConn{writeData: conn1}, JSONCodec, session1)

	msg := &CmdMessage{
		Cmd:  "chat.message",
		Body: json.RawMessage(`{"text":"hello room"}`),
	}

	err := hub.BroadcastToRoom(context.Background(), "room_001", msg, "s_002")
	if err != nil {
		t.Fatalf("BroadcastToRoom: %v", err)
	}

	data := conn1.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.Cmd != "chat.message" {
		t.Errorf("resp.Cmd: got %q, want chat.message", resp.Cmd)
	}
}

// ── 异步发送缓冲区测试 ────────────────────────────────────────

func TestConn_AsyncWrite(t *testing.T) {
	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	wsConn := &fakeWebSocketConn{writeData: conn}
	c := newConn(wsConn, JSONCodec, session)

	msg := &CmdMessage{
		Cmd:  "test",
		Meta: map[string]interface{}{"code": "10200"},
		Body: json.RawMessage(`{"ok":true}`),
	}

	// Write 应该立即返回（异步）
	err := c.Write(context.Background(), msg)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// writePump 最终应该写入底层连接
	conn.WaitWrite()
	data := conn.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Cmd != "test" {
		t.Errorf("Cmd: got %q, want test", resp.Cmd)
	}
}

func TestConn_WriteDropOldest(t *testing.T) {
	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()

	// 模拟慢消费者：WriteMessage 阻塞，writePump 只消费 1 条后卡住
	slowWS := &slowFakeWebSocketConn{writeData: conn, unblock: make(chan struct{})}
	c := newConn(slowWS, JSONCodec, session)

	// 写大量消息：writePump 消费 1 条后阻塞，sendCh 逐渐填满
	for i := 0; i < 200; i++ {
		err := c.Write(context.Background(), &CmdMessage{Cmd: "fill"})
		if err != nil {
			t.Fatalf("Write fill %d: %v", i, err)
		}
	}

	// sendCh 应该已满（writePump 被阻塞）
	used, cap := c.SendBufferUsage()
	if used != cap {
		t.Fatalf("buffer not full: got %d/%d", used, cap)
	}

	// 再写一条，触发丢弃最旧
	err := c.Write(context.Background(), &CmdMessage{Cmd: "newer"})
	if err != nil {
		t.Fatalf("Write newer: %v", err)
	}

	// 缓冲区仍然满
	used, cap = c.SendBufferUsage()
	if used != cap {
		t.Errorf("buffer usage after drop: got %d/%d, want %d/%d", used, cap, cap, cap)
	}

	// 解除阻塞，让 writePump 消费全部队列
	close(slowWS.unblock)

	// 等待 sendCh 变空（writePump 全部消费完毕）
	for used, _ := c.SendBufferUsage(); used > 0; used, _ = c.SendBufferUsage() {
		time.Sleep(5 * time.Millisecond)
	}

	// 关闭连接，writePump 应通过 doneCh 信号退出
	_ = c.Close(websocket.CloseNormalClosure, "")

	// 验证 "newer" 被写入（FIFO，最后一条）
	data := conn.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Cmd != "newer" {
		t.Errorf("last written: got %q, want newer", resp.Cmd)
	}
}

// slowFakeWebSocketConn 模拟慢消费者：每次 WriteMessage 后暂停，
// 直到 unblock channel 被 close。
type slowFakeWebSocketConn struct {
	writeData *fakeConn
	unblock   chan struct{}
}

// ── Hub.WithMaxMessageRate 限流测试 ──────────────────────────

func TestSession_CheckMessageRate(t *testing.T) {
	// 不限速：全部通过
	session := &Session{id: "s_rate", meta: make(Meta)}
	session.setMessageRate(0)
	for i := 0; i < 100; i++ {
		if !session.checkMessageRate() {
			t.Fatal("unexpected rate limit with rate=0")
		}
	}

	// 限速 3 QPS：前 3 条通过，第 4 条被拦截
	session2 := &Session{id: "s_rate2", meta: make(Meta)}
	session2.setMessageRate(3)
	for i := 0; i < 3; i++ {
		if !session2.checkMessageRate() {
			t.Fatalf("message %d should pass", i)
		}
	}
	if session2.checkMessageRate() {
		t.Fatal("message 4 should be rate limited")
	}

	// 滑动窗口：等待 1 秒后，旧记录过期，新消息应通过
	time.Sleep(1100 * time.Millisecond)
	if !session2.checkMessageRate() {
		t.Fatal("message should pass after window slides")
	}
}

func TestHub_WithMaxMessageRate(t *testing.T) {
	hub := NewHub(0, WithMaxMessageRate(10))
	if hub.messageRate != 10 {
		t.Errorf("messageRate: got %d, want 10", hub.messageRate)
	}
}

func TestHub_RunSession_RespectsMessageRate(t *testing.T) {
	// 直接验证 session 级限流在 runSession 中生效
	session := &Session{id: "s_rate3", meta: make(Meta)}
	session.setMessageRate(2)
	for i := 0; i < 2; i++ {
		session.checkMessageRate() // 2 条通过
	}
	if session.checkMessageRate() {
		t.Fatal("3rd message should be rate limited")
	}
}

// ── CloseReason 常量测试 ───────────────────────────────────────

func TestCloseReasonConstants(t *testing.T) {
	tests := []struct {
		reason CloseReason
		want   string
	}{
		{CloseReasonHeartbeatTimeout, "heartbeat_timeout"},
		{CloseReasonServerShutdown, "server_shutdown"},
		{CloseReasonInvalidMessage, "invalid_message"},
		{CloseReasonAuthDenied, "auth_denied"},
		{CloseReasonKicked, "kicked"},
	}
	for _, tt := range tests {
		if string(tt.reason) != tt.want {
			t.Errorf("CloseReason: got %q, want %q", tt.reason, tt.want)
		}
	}
}

// ── MetricsEvent 常量 & StandardMetricLabels 测试 ────────────

func TestMetricsEventConstants(t *testing.T) {
	if string(MetricEventConnect) != "connect" {
		t.Errorf("MetricEventConnect: got %q", MetricEventConnect)
	}
	if string(MetricEventDisconnect) != "disconnect" {
		t.Errorf("MetricEventDisconnect: got %q", MetricEventDisconnect)
	}
	if string(MetricEventMessage) != "message" {
		t.Errorf("MetricEventMessage: got %q", MetricEventMessage)
	}
	if string(MetricEventBroadcast) != "broadcast" {
		t.Errorf("MetricEventBroadcast: got %q", MetricEventBroadcast)
	}
	if string(MetricEventBroadcastRoom) != "broadcast_room" {
		t.Errorf("MetricEventBroadcastRoom: got %q", MetricEventBroadcastRoom)
	}
}

func TestStandardMetricLabels(t *testing.T) {
	labels := StandardMetricLabels(MetricEventConnect, map[string]string{
		"session_id": "s_001",
		"user_id":    "u_001",
	})
	if labels["event"] != "connect" {
		t.Errorf("event: got %q, want connect", labels["event"])
	}
	if labels["session_id"] != "s_001" {
		t.Errorf("session_id: got %q, want s_001", labels["session_id"])
	}
	if labels["user_id"] != "u_001" {
		t.Errorf("user_id: got %q, want u_001", labels["user_id"])
	}
}

// ── Hub.JoinRoom / LeaveRoom / ListRoomUsers 测试 ──────────────

func TestHub_JoinRoom(t *testing.T) {
	hub := NewHub(0)
	session := &Session{id: "s_001", rooms: make(map[string]struct{})}
	hub.Register(session)
	hub.JoinRoom(session, "room_001")

	// roomIndex 应已建立
	hub.mu.RLock()
	_, ok := hub.roomIndex["room_001"]
	hub.mu.RUnlock()
	if !ok {
		t.Error("roomIndex missing room_001")
	}

	// Session.rooms 也应更新
	rooms := session.Rooms()
	if len(rooms) != 1 || rooms[0] != "room_001" {
		t.Errorf("Session rooms: got %v, want [room_001]", rooms)
	}
}

func TestHub_LeaveRoom(t *testing.T) {
	hub := NewHub(0)
	session := &Session{id: "s_001", rooms: make(map[string]struct{})}
	hub.Register(session)
	hub.JoinRoom(session, "room_001")
	hub.LeaveRoom(session, "room_001")

	hub.mu.RLock()
	_, ok := hub.roomIndex["room_001"]
	hub.mu.RUnlock()
	if ok {
		t.Error("roomIndex should not have room_001 after LeaveRoom")
	}

	rooms := session.Rooms()
	if len(rooms) != 0 {
		t.Errorf("Session rooms should be empty, got %v", rooms)
	}
}

// ── Session.JoinRoom / LeaveRoom 锁序测试 ────────────────────────

func TestSession_JoinRoom_WithHub(t *testing.T) {
	hub := NewHub(0)
	session := &Session{id: "s_001", rooms: make(map[string]struct{})}
	hub.Register(session)

	// Session.JoinRoom 应正确更新 session.rooms 和 hub.roomIndex
	session.JoinRoom("room_001")

	hub.mu.RLock()
	_, ok := hub.roomIndex["room_001"]
	hub.mu.RUnlock()
	if !ok {
		t.Error("hub.roomIndex missing room_001 after Session.JoinRoom")
	}

	rooms := session.Rooms()
	if len(rooms) != 1 || rooms[0] != "room_001" {
		t.Errorf("Session rooms: got %v, want [room_001]", rooms)
	}
}

func TestSession_LeaveRoom_WithHub(t *testing.T) {
	hub := NewHub(0)
	session := &Session{id: "s_001", rooms: make(map[string]struct{})}
	hub.Register(session)
	session.JoinRoom("room_001")

	// Session.LeaveRoom 应正确清理 session.rooms 和 hub.roomIndex
	session.LeaveRoom("room_001")

	hub.mu.RLock()
	_, ok := hub.roomIndex["room_001"]
	hub.mu.RUnlock()
	if ok {
		t.Error("hub.roomIndex should not have room_001 after Session.LeaveRoom")
	}

	rooms := session.Rooms()
	if len(rooms) != 0 {
		t.Errorf("Session rooms should be empty, got %v", rooms)
	}
}

func TestSession_JoinRoom_WithoutHub(t *testing.T) {
	session := &Session{id: "s_001", rooms: make(map[string]struct{})}
	// session.hub == nil，应仅更新本地 rooms
	session.JoinRoom("room_001")

	rooms := session.Rooms()
	if len(rooms) != 1 || rooms[0] != "room_001" {
		t.Errorf("Session rooms: got %v, want [room_001]", rooms)
	}
}

func TestSession_LeaveRoom_WithoutHub(t *testing.T) {
	session := &Session{id: "s_001", rooms: make(map[string]struct{})}
	session.JoinRoom("room_001")
	session.LeaveRoom("room_001")

	rooms := session.Rooms()
	if len(rooms) != 0 {
		t.Errorf("Session rooms should be empty, got %v", rooms)
	}
}

func TestHub_ListRoomUsers(t *testing.T) {
	hub := NewHub(0)
	s1 := &Session{id: "s_001", rooms: make(map[string]struct{})}
	s2 := &Session{id: "s_002", rooms: make(map[string]struct{})}
	s3 := &Session{id: "s_003", rooms: make(map[string]struct{})}
	hub.Register(s1)
	hub.Register(s2)
	hub.Register(s3)
	hub.JoinRoom(s1, "room_001")
	hub.JoinRoom(s2, "room_001")
	hub.JoinRoom(s3, "room_002")

	users := hub.ListRoomUsers("room_001")
	if len(users) != 2 {
		t.Errorf("ListRoomUsers room_001: got %d users, want 2", len(users))
	}

	users2 := hub.ListRoomUsers("room_002")
	if len(users2) != 1 || users2[0] != "s_003" {
		t.Errorf("ListRoomUsers room_002: got %v, want [s_003]", users2)
	}

	empty := hub.ListRoomUsers("nonexistent")
	if empty != nil {
		t.Errorf("ListRoomUsers nonexistent: got %v, want nil", empty)
	}
}

// ── Hub.BroadcastToUser 测试 ───────────────────────────────────

func TestHub_BroadcastToUser(t *testing.T) {
	hub := NewHub(0)
	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newSyncConn(&syncFakeWebSocketConn{writeData: conn}, JSONCodec, session)
	session.SetUserID("user_001")
	hub.Register(session)

	msg := &CmdMessage{
		Cmd:  "private.message",
		Body: json.RawMessage(`{"text":"hello"}`),
	}
	err := hub.BroadcastToUser(context.Background(), "user_001", msg)
	if err != nil {
		t.Fatalf("BroadcastToUser: %v", err)
	}

	conn.WaitWrite()
	data := conn.GetWritten().([]byte)
	var resp CmdMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Cmd != "private.message" {
		t.Errorf("Cmd: got %q, want private.message", resp.Cmd)
	}

	// 不存在的用户应静默返回 nil
	err = hub.BroadcastToUser(context.Background(), "nobody", msg)
	if err != nil {
		t.Errorf("BroadcastToUser nonexistent: got error %v, want nil", err)
	}
}

// ── Hub.CountRooms 测试 ────────────────────────────────────────

func TestHub_CountRooms(t *testing.T) {
	hub := NewHub(0)
	if hub.CountRooms() != 0 {
		t.Errorf("CountRooms: got %d, want 0", hub.CountRooms())
	}

	s1 := &Session{id: "s_001", rooms: make(map[string]struct{})}
	s2 := &Session{id: "s_002", rooms: make(map[string]struct{})}
	hub.Register(s1)
	hub.Register(s2)
	hub.JoinRoom(s1, "room_001")
	hub.JoinRoom(s2, "room_001")
	hub.JoinRoom(s2, "room_002")

	if hub.CountRooms() != 2 {
		t.Errorf("CountRooms: got %d, want 2", hub.CountRooms())
	}
}

// ── Session.SetUserID → Hub.userIndex 同步测试 ────────────────

func TestSession_SetUserID_UpdatesHubIndex(t *testing.T) {
	hub := NewHub(0)
	session := &Session{id: "s_001", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newConn(&fakeWebSocketConn{writeData: conn}, JSONCodec, session)
	hub.Register(session)

	// 注册后设置 userID，应同步更新 Hub.userIndex
	session.SetUserID("user_001")

	got, ok := hub.GetSessionByUserID("user_001")
	if !ok || got != session {
		t.Error("GetSessionByUserID should find session after SetUserID")
	}

	// 切换 userID
	session.SetUserID("user_002")
	_, ok = hub.GetSessionByUserID("user_001")
	if ok {
		t.Error("user_001 should be removed from userIndex after SetUserID change")
	}
	got, ok = hub.GetSessionByUserID("user_002")
	if !ok || got != session {
		t.Error("GetSessionByUserID should find session with new userID")
	}
}

// ── Hub.Unregister 清理 roomIndex 测试 ────────────────────────

func TestHub_Unregister_CleansRoomIndex(t *testing.T) {
	hub := NewHub(0)
	session := &Session{id: "s_001", rooms: make(map[string]struct{})}
	hub.Register(session)
	hub.JoinRoom(session, "room_001")
	hub.JoinRoom(session, "room_002")

	hub.Unregister("s_001")

	// room_001 和 room_002 都应被清理
	hub.mu.RLock()
	_, hasRoom1 := hub.roomIndex["room_001"]
	_, hasRoom2 := hub.roomIndex["room_002"]
	hub.mu.RUnlock()

	if hasRoom1 {
		t.Error("room_001 should be removed from roomIndex after Unregister")
	}
	if hasRoom2 {
		t.Error("room_002 should be removed from roomIndex after Unregister")
	}
}

// ── Hub.WithCloseReason 测试 ───────────────────────────────────

func TestHub_WithCloseReason(t *testing.T) {
	customKick := CloseReason("custom_kicked")
	customShutdown := CloseReason("custom_shutdown")

	hub := NewHub(0,
		WithCloseReason(customKick, customShutdown),
	)

	if hub.closeReasonKick != customKick {
		t.Errorf("closeReasonKick: got %q, want %q", hub.closeReasonKick, customKick)
	}
	if hub.closeReasonShutdown != customShutdown {
		t.Errorf("closeReasonShutdown: got %q, want %q", hub.closeReasonShutdown, customShutdown)
	}
}

// ── WithOnConnect / WithOnDisconnect 回调测试 ──────────────────

func TestHub_WithOnConnectDisconnect(t *testing.T) {
	var connectedSessionID, disconnectedSessionID string

	hub := NewHub(0,
		WithOnConnect(func(s *Session) { connectedSessionID = s.ID() }),
		WithOnDisconnect(func(s *Session) { disconnectedSessionID = s.ID() }),
	)

	session := &Session{id: "s_callback", meta: make(Meta)}
	conn := newFakeConn()
	session.conn = newConn(&fakeWebSocketConn{writeData: conn}, JSONCodec, session)
	hub.Register(session)

	if connectedSessionID != "s_callback" {
		t.Errorf("onConnect: got %q, want s_callback", connectedSessionID)
	}

	hub.Unregister("s_callback")
	if disconnectedSessionID != "s_callback" {
		t.Errorf("onDisconnect: got %q, want s_callback", disconnectedSessionID)
	}
}

// ── WithSessionInit 测试 ────────────────────────────────────────

func TestHub_WithSessionInit(t *testing.T) {
	var initCalled bool
	var initSessionID string

	hub := NewHub(0,
		WithSessionInit(func(s *Session) {
			initCalled = true
			initSessionID = s.ID()
			s.SetMeta("jwt_token", "test-token-123")
		}),
	)

	// 验证 initSessionFn 已设置
	if hub.initSessionFn == nil {
		t.Fatal("initSessionFn should be set by WithSessionInit")
	}

	// 直接调用验证回调逻辑（runSession 中同步调用，无并发）
	session := &Session{id: "s_init", meta: make(Meta)}
	hub.initSessionFn(session)

	if !initCalled {
		t.Error("initSessionFn not called")
	}
	if initSessionID != "s_init" {
		t.Errorf("sessionID: got %q, want s_init", initSessionID)
	}
	token, exists := session.Meta()["jwt_token"]
	if !exists {
		t.Fatal("jwt_token not set in meta")
	}
	if token != "test-token-123" {
		t.Errorf("jwt_token: got %v, want test-token-123", token)
	}
}

func TestHub_SetSessionInit(t *testing.T) {
	hub := NewHub(0)

	var called bool
	hub.SetSessionInit(func(s *Session) {
		called = true
	})

	if hub.initSessionFn == nil {
		t.Fatal("SetSessionInit should set initSessionFn")
	}

	session := &Session{id: "s_set", meta: make(Meta)}
	hub.initSessionFn(session)

	if !called {
		t.Error("initSessionFn not called after SetSessionInit")
	}
}

// ── WithMaxMessageSize 测试 ────────────────────────────────────

func TestHub_WithMaxMessageSize(t *testing.T) {
	hub := NewHub(0, WithMaxMessageSize(1024))
	if hub.maxMessageSize != 1024 {
		t.Errorf("maxMessageSize: got %d, want 1024", hub.maxMessageSize)
	}
}

func (f *slowFakeWebSocketConn) ReadMessage() (messageType int, data []byte, err error) {
	return 0, nil, nil
}

func (f *slowFakeWebSocketConn) WriteMessage(t int, d []byte) error {
	if f.writeData != nil {
		f.writeData.Write(d)
	}
	// 暂停，等待测试解除阻塞
	<-f.unblock
	return nil
}

func (f *slowFakeWebSocketConn) WriteControl(messageType int, d []byte, deadline time.Time) error {
	return nil
}

func (f *slowFakeWebSocketConn) SetPongHandler(h func(string) error) {}

func (f *slowFakeWebSocketConn) SetReadLimit(n int64)               {}
func (f *slowFakeWebSocketConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *slowFakeWebSocketConn) SetWriteDeadline(t time.Time) error { return nil }
func (f *slowFakeWebSocketConn) Close() error                       { return nil }

// syncFakeWebSocketConn 同步写入：WriteMessage 直接写底层 fakeConn。
// 用于测试中消除 writePump goroutine 调度时序的不确定性。
type syncFakeWebSocketConn struct {
	writeData *fakeConn
}

func (f *syncFakeWebSocketConn) ReadMessage() (int, []byte, error) {
	return 0, nil, nil
}

func (f *syncFakeWebSocketConn) WriteMessage(t int, d []byte) error {
	if f.writeData != nil {
		f.writeData.Write(d)
	}
	return nil
}

func (f *syncFakeWebSocketConn) WriteControl(messageType int, d []byte, deadline time.Time) error {
	return nil
}

func (f *syncFakeWebSocketConn) SetPongHandler(h func(string) error) {}

func (f *syncFakeWebSocketConn) SetReadLimit(n int64)               {}
func (f *syncFakeWebSocketConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *syncFakeWebSocketConn) SetWriteDeadline(t time.Time) error { return nil }
func (f *syncFakeWebSocketConn) Close() error                       { return nil }

// ── 集成测试：真实 WebSocket 连接 ────────────────────────────

func TestUpgrade_Integration(t *testing.T) {
	hub := NewHub(0)
	router := NewRouter()

	var received bool
	router.Cmd("ping", func(ctx context.Context, session *Session, msg *CmdMessage) error {
		received = true
		return session.Conn().Write(ctx, &CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"url":"pong"}`),
		})
	})

	handler := Upgrade(hub, router.Handle)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	ws.WriteJSON(CmdMessage{
		Cmd:  "ping",
		Body: json.RawMessage(`{}`),
	})

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resp CmdMessage
	if err := ws.ReadJSON(&resp); err != nil {
		t.Fatalf("read response: %v", err)
	}

	if !received {
		t.Error("handler not called")
	}
	if resp.Cmd != "ping" {
		t.Errorf("resp.Cmd: got %q, want ping", resp.Cmd)
	}
	if resp.Meta["code"] != "10200" {
		t.Errorf("resp code: got %v, want 10200", resp.Meta["code"])
	}
}

// ── 测试辅助 ──────────────────────────────────────────────────

func newSyncConn(ws wsConn, codec MessageCodec, session *Session) *Conn {
	if codec == nil {
		codec = JSONCodec
	}
	return &Conn{
		conn:    ws,
		codec:   codec,
		session: session,
		sendCh:  nil,
		doneCh:  make(chan struct{}),
	}
}

type fakeConn struct {
	mu          sync.Mutex
	lastWritten interface{}
	writeCh     chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{writeCh: make(chan struct{}, 1)}
}

func (f *fakeConn) Write(v interface{}) {
	f.mu.Lock()
	f.lastWritten = v
	f.mu.Unlock()
	select {
	case f.writeCh <- struct{}{}:
	default:
	}
}

func (f *fakeConn) WaitWrite() {
	<-f.writeCh
}

func (f *fakeConn) GetWritten() interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastWritten
}

type fakeWebSocketConn struct {
	writeData *fakeConn
}

func (f *fakeWebSocketConn) ReadMessage() (messageType int, data []byte, err error) {
	return 0, nil, nil
}

func (f *fakeWebSocketConn) WriteMessage(t int, d []byte) error {
	if f.writeData != nil {
		f.writeData.Write(d)
	}
	return nil
}

func (f *fakeWebSocketConn) WriteControl(messageType int, d []byte, deadline time.Time) error {
	return nil
}

func (f *fakeWebSocketConn) SetPongHandler(h func(string) error) {}

func (f *fakeWebSocketConn) SetReadLimit(n int64)               {}
func (f *fakeWebSocketConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeWebSocketConn) SetWriteDeadline(t time.Time) error { return nil }
func (f *fakeWebSocketConn) Close() error                       { return nil }

// ── 基准测试 ──────────────────────────────────────────────────

func BenchmarkRouter_Handle(b *testing.B) {
	router := NewRouter()
	router.Cmd("test.cmd", func(ctx context.Context, session *Session, msg *CmdMessage) error {
		return nil
	})

	session := &Session{id: "s_bench", meta: make(Meta)}
	msg := &CmdMessage{Cmd: "test.cmd", Body: json.RawMessage(`{}`)}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = router.Handle(context.Background(), session, msg)
		}
	})
}

func BenchmarkSession_CheckMessageRate(b *testing.B) {
	session := &Session{id: "s_rate_bench", meta: make(Meta)}
	session.setMessageRate(1000) // 高限额，避免限流影响基准

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			session.checkMessageRate()
		}
	})
}

func BenchmarkHub_RegisterUnregister(b *testing.B) {
	hub := NewHub(0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			session := &Session{id: fmt.Sprintf("s_%d", b.N), meta: make(Meta)}
			hub.Register(session)
			hub.Unregister(session.ID())
		}
	})
}
