package websocketx

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ── Hub 核心 ──────────────────────────────────────────────────

// Hub 管理所有活跃的 WebSocket 连接。
type Hub struct {
	mu                  sync.RWMutex
	sessions            map[string]*Session
	userIndex           map[string]*Session            // userID -> session
	roomIndex           map[string]map[string]struct{} // roomID -> sessionID set
	maxConn             int
	semaphore           chan struct{} // 连接数限制信号量
	heartbeatInterval   time.Duration
	heartbeatPongWait   time.Duration
	maxMessageSize      int64
	messageRate         int // 单 Session 每秒最大消息数，0 表示不限
	closeReasonKick     CloseReason
	closeReasonShutdown CloseReason
	onConnect           func(*Session)
	onDisconnect        func(*Session)
	metrics             hubMetrics
	wg                  sync.WaitGroup // 跟踪活跃的 runSession goroutine
	handlerTimeout      time.Duration  // 单条消息处理超时，0 表示不限制
	nodeID              string         // 当前 AP 节点唯一标识，用于跨 AP 广播去重

	// 全局连接初始化回调（所有连接共享，可通过 WithSessionInit 配置）。
	// 读写通过 initMu 保护，per-request 的 init 通过 context 传递。
	initSessionFn func(*Session)
	initMu        sync.RWMutex

	// 跨 AP Session 注册表（将 userID 映射到 AP 节点地址）
	sessionRegistry SessionRegistry

	// 跨 AP 广播总线（Redis Pub/Sub）
	broadcastBus BroadcastBus

	// 测试用：Session 注册完成通知 channel
	registerCh chan string
}

// contextKey 用于在 context 中传递 per-connection 的初始化回调。
type contextKey string

const SessionInitCtxKey contextKey = "websocketx.sessionInit"

// NewHub 创建一个 Hub。
// maxConn 是最大并发连接数，0 表示不限制。
func NewHub(maxConn int, opts ...HubOption) *Hub {
	h := &Hub{
		sessions:            make(map[string]*Session),
		userIndex:           make(map[string]*Session),
		roomIndex:           make(map[string]map[string]struct{}),
		maxConn:             maxConn,
		closeReasonKick:     CloseReasonKicked,
		closeReasonShutdown: CloseReasonServerShutdown,
	}
	if maxConn > 0 {
		h.semaphore = make(chan struct{}, maxConn)
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// handleConnection 处理一条新连接的完整生命周期。
func (h *Hub) handleConnection(ctx context.Context, conn wsConn, codec MessageCodec, handler MessageHandler) error {
	// 连接数限制（非阻塞）
	if h.semaphore != nil {
		select {
		case h.semaphore <- struct{}{}:
			defer func() { <-h.semaphore }()
		default:
			return ErrTooManyConnections
		}
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.runSession(ctx, conn, codec, handler)
	}()
	return nil
}

// runSession 执行单条连接的完整生命周期：
//  1. 创建 Session 并应用初始化回调（注入 JWT token 等）
//  2. 注册到 Hub（内存 sessions/userIndex + Redis SessionRegistry）
//  3. 启动心跳保活（Ping/Pong）
//  4. 读循环：逐条读取消息 → 速率检查 → 调用 handler
//  5. 断开后注销（清理内存索引 + Redis SessionRegistry）
func (h *Hub) runSession(ctx context.Context, conn wsConn, codec MessageCodec, handler MessageHandler) {
	session := newSession(conn, codec)
	session.hub = h

	// 应用连接级初始化回调（优先 context，回退到 Hub 全局）
	if initFn, ok := ctx.Value(SessionInitCtxKey).(func(*Session)); ok {
		initFn(session)
	} else {
		h.initMu.RLock()
		fn := h.initSessionFn
		h.initMu.RUnlock()
		if fn != nil {
			fn(session)
		}
	}

	// 应用 MaxMessageSize
	if h.maxMessageSize > 0 {
		conn.SetReadLimit(h.maxMessageSize)
	}

	// 应用消息速率限制
	session.setMessageRate(h.messageRate)

	// 注册
	h.register(session)

	// 心跳（仅当配置了 interval 和 pongWait 时启用）
	var hb *Heartbeat
	if h.heartbeatInterval > 0 && h.heartbeatPongWait > 0 {
		hb = StartHeartbeat(ctx, session, h.heartbeatInterval, h.heartbeatPongWait)
	}

	// 读循环
	for {
		msg, err := session.conn.Read(ctx)
		if err != nil {
			// 区分正常关闭和异常断开
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				break
			}
			// 异常断开：向客户端发送错误响应后断开
			_ = session.Conn().Write(ctx, &CmdMessage{
				Cmd:  "unknown",
				Meta: map[string]interface{}{"code": "10500", "message": err.Error()},
			})
			break
		}

		// 消息速率检查
		if !session.checkMessageRate() {
			_ = session.Conn().Write(ctx, &CmdMessage{
				Cmd:  msg.Cmd,
				Meta: map[string]interface{}{"code": "10429", "message": "rate limit exceeded"},
			})
			session.Conn().Flush()
			break
		}

		// 应用 handler 超时
		handlerCtx := ctx
		var handlerCancel context.CancelFunc
		if h.handlerTimeout > 0 {
			handlerCtx, handlerCancel = context.WithTimeout(ctx, h.handlerTimeout)
		}

		start := time.Now()
		handlerErr := handler(handlerCtx, session, msg)
		if handlerCancel != nil {
			handlerCancel()
		}
		duration := time.Since(start)

		if h.metrics.onMessage != nil {
			h.metrics.onMessage(string(MetricEventMessage), StandardMetricLabels(MetricEventMessage, map[string]string{
				"session_id":  session.ID(),
				"cmd":         msg.Cmd,
				"duration_ms": fmt.Sprintf("%d", duration.Milliseconds()),
				"error":       fmt.Sprintf("%v", handlerErr),
			}))
		}

		if handlerErr != nil {
			// 向客户端发送错误响应后再断开连接
			_ = session.Conn().Write(ctx, &CmdMessage{
				Cmd:  msg.Cmd,
				Meta: map[string]interface{}{"code": "10500", "message": handlerErr.Error()},
			})
			session.Conn().Flush()
			break
		}
	}

	if hb != nil {
		hb.Stop()
	}

	// 注销
	h.unregister(session)
}

// register 将 Session 注册到 Hub（内部方法）。
func (h *Hub) register(session *Session) {
	h.mu.Lock()
	h.sessions[session.id] = session
	userID := session.userID
	if userID != "" {
		h.userIndex[userID] = session
	}
	h.mu.Unlock()

	// 向 Redis 注册 Session 映射（userID 已快照，避免并发 SetUserID 竞态）
	if userID != "" && h.sessionRegistry != nil {
		_ = h.sessionRegistry.Register(context.Background(), "", userID, h.nodeID)
	}

	// 通知注册完成（测试同步用）
	if h.registerCh != nil {
		select {
		case h.registerCh <- session.id:
		default:
		}
	}

	h.triggerOnConnect(session)
}

// unregister 从 Hub 注销 Session，清理房间映射和 userIndex。
func (h *Hub) unregister(session *Session) {
	h.mu.Lock()
	delete(h.sessions, session.id)
	userID := session.userID
	if userID != "" {
		delete(h.userIndex, userID)
	}

	for roomID := range session.rooms {
		if members, ok := h.roomIndex[roomID]; ok {
			delete(members, session.id)
			if len(members) == 0 {
				delete(h.roomIndex, roomID)
			}
		}
	}
	h.mu.Unlock()

	// 从 Redis 注销 Session 映射（userID 已快照，避免并发 SetUserID 竞态）
	if userID != "" && h.sessionRegistry != nil {
		_ = h.sessionRegistry.Unregister(context.Background(), "", userID)
	}

	h.triggerOnDisconnect(session)
}

// Register 注册一个新 Session 到 Hub。
func (h *Hub) Register(session *Session) {
	session.setHub(h)
	h.register(session)
}

// Unregister 从 Hub 注销一个 Session。
func (h *Hub) Unregister(id string) {
	h.mu.Lock()
	session, ok := h.sessions[id]
	h.mu.Unlock()

	if !ok {
		return
	}
	session.setHub(nil)
	h.unregister(session)
}

// GetSession 根据 ID 获取 Session。
func (h *Hub) GetSession(id string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[id]
	return s, ok
}

// GetSessionByUserID 根据用户 ID 获取 Session。
func (h *Hub) GetSessionByUserID(userID string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.userIndex[userID]
	return s, ok
}

// Count 返回当前连接数。
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

// SetSessionInit 设置连接建立后的 Session 初始化回调。
// 回调在每个新连接创建 Session 后、消息处理开始前执行一次。
// 线程安全：可通过 WithSessionInit 或 SetSessionInit 设置。
func (h *Hub) SetSessionInit(fn func(*Session)) {
	h.initMu.Lock()
	h.initSessionFn = fn
	h.initMu.Unlock()
}

// SetNodeID 设置当前 AP 节点唯一标识（用于跨 AP 广播去重）。
func (h *Hub) SetNodeID(nodeID string) {
	h.mu.Lock()
	h.nodeID = nodeID
	h.mu.Unlock()
}

// SetSessionRegistry 设置 Session 注册表（用于跨 AP 定位用户连接）。
func (h *Hub) SetSessionRegistry(registry SessionRegistry) {
	h.sessionRegistry = registry
}

// SetBroadcastBus 设置跨 AP 广播总线。
func (h *Hub) SetBroadcastBus(bus BroadcastBus) {
	h.broadcastBus = bus
}

// Shutdown 优雅关闭：关闭所有现有连接，等待所有 goroutine 退出，返回第一个错误（如有）。
func (h *Hub) Shutdown(ctx context.Context) error {
	h.mu.RLock()
	sessions := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.RUnlock()

	var firstErr error
	for _, s := range sessions {
		if err := s.Conn().Close(websocket.CloseGoingAway, string(h.closeReasonShutdown)); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// 等待所有 runSession goroutine 退出
	h.wg.Wait()

	// 关闭跨 AP 广播总线
	if h.broadcastBus != nil {
		_ = h.broadcastBus.Close()
	}

	return firstErr
}

// Wait 等待所有 runSession goroutine 退出。
// 用于测试中确保异步操作（如 Kick 触发的 unregister）已完成。
func (h *Hub) Wait() {
	h.wg.Wait()
}
