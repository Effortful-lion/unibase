package websocketx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gorilla/websocket"
)

// CmdMessage 是 WebSocket 传输的统一消息格式（JSON）。
//
// wire 格式：
//
//	{
//	  "cmd": "file.upload",
//	  "meta": { "requestId": "abc123", "userID": "u_001" },
//	  "body": { "fileName": "a.jpg", "fileSize": 10240 }
//	}
type CmdMessage struct {
	Cmd  string                 `json:"cmd"`
	Meta map[string]interface{} `json:"meta"`
	Body json.RawMessage        `json:"body"`
}

// Bind 将 Body 反序列化到目标结构体。
//
//	var req UploadReq
//	if err := msg.Bind(&req); err != nil { return err }
func (m *CmdMessage) Bind(v interface{}) error {
	return json.Unmarshal(m.Body, v)
}

// Reply 构造错误响应并写入连接。
// code 为业务状态码（如 "10500"），err 为错误详情。
func Reply(ctx context.Context, conn *Conn, msg *CmdMessage, code string, err error) error {
	resp := &CmdMessage{
		Cmd:  msg.Cmd,
		Meta: map[string]interface{}{"code": code},
	}
	if err != nil {
		resp.Meta["message"] = err.Error()
	}
	return conn.Write(ctx, resp)
}

// ── 关闭原因常量 ──────────────────────────────────────────────

// CloseReason 是标准 WebSocket 关闭原因。
type CloseReason string

const (
	// CloseReasonHeartbeatTimeout 心跳超时（Pong 未在 pongWait 内到达）。
	CloseReasonHeartbeatTimeout CloseReason = "heartbeat_timeout"
	// CloseReasonServerShutdown 服务端优雅关闭。
	CloseReasonServerShutdown CloseReason = "server_shutdown"
	// CloseReasonInvalidMessage 非法报文（编解码失败或格式错误）。
	CloseReasonInvalidMessage CloseReason = "invalid_message"
	// CloseReasonAuthDenied 权限校验未通过（中间件拦截）。
	CloseReasonAuthDenied CloseReason = "auth_denied"
	// CloseReasonKicked 被服务端主动踢下线。
	CloseReasonKicked CloseReason = "kicked"
)

// ── 错误定义 ──────────────────────────────────────────────────

var (
	// ErrCmdNotFound 表示收到的 cmd 没有注册对应的 Handler。
	ErrCmdNotFound = NewError("cmd not found")

	// ErrInvalidMessage 表示收到的消息格式不合法。
	ErrInvalidMessage = NewError("invalid message")

	// ErrTooManyConnections 表示达到最大连接数上限。
	ErrTooManyConnections = NewError("too many connections")
)

// Error 是 websocketx 的领域错误类型。
type Error struct {
	msg string
}

// NewError 创建一个 Error。
func NewError(msg string) *Error {
	return &Error{msg: msg}
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	return e.msg
}

// MetricsEvent 是监控埋点事件名称。
type MetricsEvent string

const (
	MetricEventConnect       MetricsEvent = "connect"
	MetricEventDisconnect    MetricsEvent = "disconnect"
	MetricEventMessage       MetricsEvent = "message"
	MetricEventBroadcast     MetricsEvent = "broadcast"
	MetricEventBroadcastRoom MetricsEvent = "broadcast_room"
)

// StandardMetricLabels 返回标准 metrics 标签集合，减少调用方 switch 负担。
func StandardMetricLabels(event MetricsEvent, extras ...map[string]string) map[string]string {
	labels := map[string]string{
		"event": string(event),
	}
	for _, m := range extras {
		for k, v := range m {
			labels[k] = v
		}
	}
	return labels
}

// MessageCodec 定义消息的编解码方式。
// 默认实现为 JSONCodec，用户可实现自定义编解码器（如 protobuf、msgpack）。
type MessageCodec interface {
	// Encode 将 CmdMessage 编码为字节。
	Encode(*CmdMessage) ([]byte, error)
	// Decode 将字节解码为 CmdMessage。
	Decode([]byte) (*CmdMessage, error)
	// MessageType 返回对应的 WebSocket 消息类型（TextMessage 或 BinaryMessage）。
	MessageType() int
}

// JSONCodec 是默认的 JSON 编解码器。
var JSONCodec MessageCodec = jsonCodec{}

type jsonCodec struct{}

func (jsonCodec) Encode(msg *CmdMessage) ([]byte, error) {
	return json.Marshal(msg)
}

func (jsonCodec) Decode(data []byte) (*CmdMessage, error) {
	var msg CmdMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("websocketx: decode json: %w", err)
	}
	return &msg, nil
}

func (jsonCodec) MessageType() int {
	return websocket.TextMessage
}
