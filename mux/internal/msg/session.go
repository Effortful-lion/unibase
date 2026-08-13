package msg

import (
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"

	"github.com/Effortful-lion/unibase/mux/internal/websocketx"
)

// WSSession 封装 websocketx.Session，实现 mux.Session 接口。
type WSSession struct {
	session *websocketx.Session
}

// NewWSSession 从 websocketx.Session 创建 WSSession。
func NewWSSession(s *websocketx.Session) *WSSession {
	return &WSSession{session: s}
}

// ID 返回 WebSocket 连接唯一标识。
func (s *WSSession) ID() string {
	return s.session.ID()
}

// UserID 返回关联的用户 ID。
func (s *WSSession) UserID() string {
	return s.session.UserID()
}

// SetState 设置业务自定义状态。
func (s *WSSession) SetState(key string, value any) {
	s.session.SetState(key, value)
}

// GetState 获取业务自定义状态。
func (s *WSSession) GetState(key string) (any, bool) {
	return s.session.GetState(key)
}

// Conn 返回底层连接，用于直接写响应。
func (s *WSSession) Conn() *websocketx.Conn {
	return s.session.Conn()
}

// Raw 返回原始 websocketx.Session。
func (s *WSSession) Raw() *websocketx.Session {
	return s.session
}

// Meta 返回连接元数据（Upgrade 阶段注入，只读）。
func (s *WSSession) Meta() map[string]interface{} {
	return s.session.Meta()
}

// ── HTTP 请求级 Session ────────────────────────────────────

var requestIDSeq uint64

func nextRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	n := atomic.AddUint64(&requestIDSeq, 1)
	b[0] ^= byte(n >> 0)
	b[1] ^= byte(n >> 8)
	b[2] ^= byte(n >> 16)
	b[3] ^= byte(n >> 24)
	return hex.EncodeToString(b[:])
}

// HTTPRequestSession 是 HTTP 请求级的 Session 实现。
// 生命周期仅限当前请求，请求结束后销毁。
type HTTPRequestSession struct {
	id     string
	userID string
	state  map[string]any
}

// NewHTTPRequestSession 创建 HTTP 请求级 Session。
func NewHTTPRequestSession() *HTTPRequestSession {
	return &HTTPRequestSession{
		id:    nextRequestID(),
		state: make(map[string]any),
	}
}

// WithUserID 设置用户 ID 并返回 Session，用于认证中间件。
func (s *HTTPRequestSession) WithUserID(userID string) *HTTPRequestSession {
	s.userID = userID
	return s
}

// ID 返回请求唯一标识。
func (s *HTTPRequestSession) ID() string {
	return s.id
}

// UserID 返回用户 ID。
func (s *HTTPRequestSession) UserID() string {
	return s.userID
}

// SetState 设置请求级状态。
func (s *HTTPRequestSession) SetState(key string, value any) {
	s.state[key] = value
}

// GetState 获取请求级状态。
func (s *HTTPRequestSession) GetState(key string) (any, bool) {
	v, ok := s.state[key]
	return v, ok
}
