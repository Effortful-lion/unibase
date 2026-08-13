package websocketx

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Meta 是连接元数据，在 Upgrade 阶段注入，后续只读。
type Meta map[string]interface{}

// Session 封装单条 WebSocket 连接的状态。
type Session struct {
	id           string
	userID       string
	conn         *Conn
	meta         Meta
	state        map[string]interface{}
	rooms        map[string]struct{}
	lastPongTime time.Time
	mu           sync.RWMutex
	hub          *Hub // 所属 Hub，用于 JoinRoom/LeaveRoom 时原子更新 roomIndex 和 userIndex

	// 消息速率限制（QPS）
	messageRate  int         // 每秒最大消息数，0 表示不限
	messageTimes []time.Time // 滑动窗口：最近收到消息的时间戳
	messageMu    sync.Mutex  // 独立锁，避免与业务状态锁争抢
}

func newSession(ws wsConn, codec MessageCodec) *Session {
	session := &Session{
		id:           generateID(),
		meta:         make(Meta),
		state:        make(map[string]interface{}),
		rooms:        make(map[string]struct{}),
		lastPongTime: time.Now(),
	}
	session.conn = newConn(ws, codec, session)
	return session
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ID 返回 Session 的唯一标识。
func (s *Session) ID() string {
	return s.id
}

// UserID 返回关联的用户 ID，空表示未绑定。
func (s *Session) UserID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userID
}

// SetUserID 绑定用户 ID。
// 若 Session 已注册到 Hub，同步更新 Hub.userIndex。
func (s *Session) SetUserID(userID string) {
	s.mu.Lock()
	oldID := s.userID
	s.userID = userID
	s.mu.Unlock()

	if s.hub != nil && oldID != userID {
		s.hub.mu.Lock()
		if oldID != "" {
			delete(s.hub.userIndex, oldID)
		}
		if userID != "" {
			s.hub.userIndex[userID] = s
		}
		s.hub.mu.Unlock()
	}
}

// Conn 返回底层的 Conn 封装。
func (s *Session) Conn() *Conn {
	return s.conn
}

// Meta 返回元数据的副本（只读，防止外部修改内部状态）。
func (s *Session) Meta() Meta {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(Meta, len(s.meta))
	for k, v := range s.meta {
		result[k] = v
	}
	return result
}

// SetMeta 设置元数据。
func (s *Session) SetMeta(key string, val interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta[key] = val
}

// GetState 获取业务自定义状态。
func (s *Session) GetState(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.state[key]
	return val, ok
}

// SetState 设置业务自定义状态。
func (s *Session) SetState(key string, val interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = val
}

// JoinRoom 加入房间。
// 若 Session 已绑定 Hub，则原子更新 Hub.roomIndex；否则仅更新本地 rooms。
func (s *Session) JoinRoom(roomID string) {
	s.mu.Lock()
	s.rooms[roomID] = struct{}{}

	if s.hub != nil {
		s.mu.Unlock()
		s.hub.JoinRoom(s, roomID)
		s.mu.Lock()
	}
	s.mu.Unlock()
}

// LeaveRoom 离开房间。
// 若 Session 已绑定 Hub，则原子更新 Hub.roomIndex；否则仅更新本地 rooms。
func (s *Session) LeaveRoom(roomID string) {
	s.mu.Lock()
	delete(s.rooms, roomID)

	if s.hub != nil {
		s.mu.Unlock()
		s.hub.LeaveRoom(s, roomID)
		s.mu.Lock()
	}
	s.mu.Unlock()
}

// Rooms 返回当前所在的所有房间 ID。
func (s *Session) Rooms() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rooms := make([]string, 0, len(s.rooms))
	for r := range s.rooms {
		rooms = append(rooms, r)
	}
	return rooms
}

// setHub 绑定所属 Hub（由 Hub 内部调用，外部不应直接调用）。
func (s *Session) setHub(h *Hub) {
	s.hub = h
}

// updatePongTime 更新最后一次收到 Pong 的时间（由 Pong handler 调用）。
func (s *Session) updatePongTime() {
	s.mu.Lock()
	s.lastPongTime = time.Now()
	s.mu.Unlock()
}

// pingTimeout 检查是否超过 pongWait 未收到 Pong。
func (s *Session) pingTimeout(pongWait time.Duration) bool {
	s.mu.RLock()
	last := s.lastPongTime
	s.mu.RUnlock()
	return time.Since(last) > pongWait
}

// setMessageRate 设置每秒最大消息数（0 表示不限）。
func (s *Session) setMessageRate(rate int) {
	s.messageMu.Lock()
	defer s.messageMu.Unlock()
	s.messageRate = rate
	if rate > 0 {
		s.messageTimes = make([]time.Time, 0, rate*2)
	}
}

// checkMessageRate 检查当前消息是否超出速率限制。
// 返回 true 表示允许通过，false 表示超出限流。
func (s *Session) checkMessageRate() bool {
	s.messageMu.Lock()
	defer s.messageMu.Unlock()

	if s.messageRate <= 0 {
		return true
	}

	now := time.Now()

	// 清理 1 秒前的旧记录（滑动窗口）
	cutoff := now.Add(-time.Second)
	valid := 0
	for i := range s.messageTimes {
		if s.messageTimes[i].After(cutoff) {
			s.messageTimes[valid] = s.messageTimes[i]
			valid++
		}
	}
	s.messageTimes = append([]time.Time{}, s.messageTimes[:valid]...)

	// 检查是否已触顶
	if valid >= s.messageRate {
		return false
	}

	// 记录本次消息
	s.messageTimes = append(s.messageTimes, now)
	return true
}

// MessageRate 返回当前 Session 的消息速率限制（每秒消息数），0 表示不限。
func (s *Session) MessageRate() int {
	s.messageMu.Lock()
	defer s.messageMu.Unlock()
	return s.messageRate
}
