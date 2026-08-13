package websocketx

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// UpgradeOption 配置 Upgrade 的可选参数。
type UpgradeOption func(*upgradeConfig)

type upgradeConfig struct {
	checkOrigin      func(*http.Request) bool
	handshakeTimeout time.Duration
	readBufferSize   int
	writeBufferSize  int
	maxMessageSize   int64
	codec            MessageCodec
}

// WithCheckOrigin 自定义跨域校验函数。
// 默认拒绝所有跨域请求。
func WithCheckOrigin(fn func(*http.Request) bool) UpgradeOption {
	return func(c *upgradeConfig) {
		c.checkOrigin = fn
	}
}

// WithHandshakeTimeout 设置握手超时。
func WithHandshakeTimeout(d time.Duration) UpgradeOption {
	return func(c *upgradeConfig) {
		c.handshakeTimeout = d
	}
}

// WithReadBufferSize 设置 WebSocket 读缓冲区大小（字节）。
func WithReadBufferSize(n int) UpgradeOption {
	return func(c *upgradeConfig) {
		c.readBufferSize = n
	}
}

// WithWriteBufferSize 设置 WebSocket 写缓冲区大小（字节）。
func WithWriteBufferSize(n int) UpgradeOption {
	return func(c *upgradeConfig) {
		c.writeBufferSize = n
	}
}

// WithUpgradeMaxMessageSize 设置单条消息的最大 Payload 大小（字节），防止攻击。
// 0 表示不限制（默认）。建议生产环境设置合理上限（如 1MB）。
func WithUpgradeMaxMessageSize(n int64) UpgradeOption {
	return func(c *upgradeConfig) {
		c.maxMessageSize = n
	}
}

// WithCodec 设置消息编解码器，默认 JSONCodec。
// 用于支持非 JSON 协议扩展。
func WithCodec(codec MessageCodec) UpgradeOption {
	return func(c *upgradeConfig) {
		c.codec = codec
	}
}

// Upgrade 创建一个 http.Handler，负责将 HTTP 请求升级为 WebSocket 连接。
//
// hub: 连接管理（注册、注销、广播）
// handler: 消息分发回调（Router.Handle 实现了该接口）
// opts: 升级配置选项
//
// 内部流程：
//   - CheckOrigin 校验
//   - 升级为 WebSocket
//   - 创建 Session（生成 ID）
//   - Hub 注册
//   - 启动心跳
//   - 启动读循环，逐条消息调用 handler
//   - 断开后注销并停止心跳
func Upgrade(hub *Hub, handler MessageHandler, opts ...UpgradeOption) http.Handler {
	cfg := upgradeConfig{
		// 默认拒绝跨域请求：仅允许相同来源（Origin 为空或 Host 匹配）
		checkOrigin: defaultCheckOrigin,
		codec:       JSONCodec,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	upgrader := websocket.Upgrader{
		CheckOrigin:      cfg.checkOrigin,
		HandshakeTimeout: cfg.handshakeTimeout,
		ReadBufferSize:   cfg.readBufferSize,
		WriteBufferSize:  cfg.writeBufferSize,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		if err := hub.handleConnection(r.Context(), conn, cfg.codec, handler); err != nil {
			conn.Close()
			return
		}
	})
}

// defaultCheckOrigin 默认跨域校验：仅允许相同来源或无 Origin 的请求。
// 生产环境应通过 WithCheckOrigin 显式配置允许的来源列表。
func defaultCheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // 无 Origin 视为同源（如 curl、移动端）
	}
	return origin == "http://"+r.Host || origin == "https://"+r.Host
}

// DefaultCheckOrigin 返回默认的 WebSocket 跨域校验函数。
// 该函数仅允许相同来源或无 Origin 的请求。
func DefaultCheckOrigin(r *http.Request) bool {
	return defaultCheckOrigin(r)
}
