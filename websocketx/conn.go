package websocketx

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsConn 抽象底层 WebSocket 连接，便于测试时注入 mock。
type wsConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	WriteControl(int, []byte, time.Time) error
	SetPongHandler(func(string) error)
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetReadLimit(n int64)
	Close() error
}

// Conn 封装单条 WebSocket 连接的读写能力。
//
// 写路径采用异步缓冲：Write 将编码后的字节推入 sendCh 立即返回，
// 独立 goroutine（writePump）消费 sendCh 写入底层连接。
// 队列满时丢弃最旧消息，避免阻塞调用方。
type Conn struct {
	conn    wsConn
	codec   MessageCodec
	session *Session
	writeMu sync.Mutex // 保护 Close 与 writePump 的交替写
	sendCh  chan []byte
	doneCh  chan struct{}
	closed  bool
}

const defaultSendBufferSize = 64

func newConn(ws wsConn, codec MessageCodec, session *Session) *Conn {
	if codec == nil {
		codec = JSONCodec
	}
	c := &Conn{
		conn:    ws,
		codec:   codec,
		session: session,
		sendCh:  make(chan []byte, defaultSendBufferSize),
		doneCh:  make(chan struct{}),
	}
	go c.writePump()
	return c
}

// Read 从连接读取一条 CmdMessage。
// ctx 的 Deadline 会被透传到底层 ReadMessage。
func (c *Conn) Read(ctx context.Context) (*CmdMessage, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}

	_, data, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	msg, err := c.codec.Decode(data)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// Write 向连接写入一条 CmdMessage。
// 默认异步：经 codec 序列化后推入 sendCh，writePump 独立 goroutine 负责实际写入。
// sendCh 满时丢弃最旧消息，保证不会阻塞调用方。
func (c *Conn) Write(ctx context.Context, msg *CmdMessage) error {
	data, err := c.codec.Encode(msg)
	if err != nil {
		return err
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(deadline)
		defer c.conn.SetWriteDeadline(time.Time{})
	}

	if c.sendCh == nil {
		c.writeMu.Lock()
		_ = c.conn.WriteMessage(websocket.TextMessage, data)
		c.writeMu.Unlock()
		return nil
	}

	select {
	case c.sendCh <- data:
		return nil
	case <-c.doneCh:
		return fmt.Errorf("websocketx: write on closed connection")
	default:
		// 队列满：丢弃最旧消息，保证新消息不被阻塞
		select {
		case old := <-c.sendCh:
			_ = old
		default:
		}
		select {
		case c.sendCh <- data:
			return nil
		case <-c.doneCh:
			return fmt.Errorf("websocketx: write on closed connection")
		}
	}
}

// Ping 发送 Ping 帧（同步，绕过 sendCh 保证及时性）。
func (c *Conn) Ping(ctx context.Context) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(deadline)
		defer c.conn.SetWriteDeadline(time.Time{})
	}

	return c.conn.WriteMessage(websocket.PingMessage, nil)
}

// Close 发送 Close 帧并关闭连接。
func (c *Conn) Close(code int, reason string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	// 先通知 writePump 退出，再清空队列（避免 writePump 持有锁时死锁）
	close(c.doneCh)

	// 非阻塞清空待发送队列
	for {
		select {
		case <-c.sendCh:
		default:
			goto drained
		}
	}
drained:

	_ = c.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(5*time.Second),
	)

	return c.conn.Close()
}

// Reply 构造错误响应并写入连接。
// code 为业务状态码（如 "10500"），err 为错误详情。
func (c *Conn) Reply(ctx context.Context, msg *CmdMessage, code string, err error) error {
	return Reply(ctx, c, msg, code, err)
}

// Session 返回连接关联的 Session。
func (c *Conn) Session() *Session {
	return c.session
}

// SetPongHandler 设置 Pong 帧处理函数。
func (c *Conn) SetPongHandler(h func(string) error) error {
	c.conn.SetPongHandler(h)
	return nil
}

// writePump 独立 goroutine，消费 sendCh 写入底层连接。
func (c *Conn) writePump() {
	for {
		// 优先检查退出信号，避免持有锁时无法响应关闭
		select {
		case <-c.doneCh:
			// drain 剩余消息后退出
			for {
				select {
				case data := <-c.sendCh:
					c.writeMu.Lock()
					_ = c.conn.WriteMessage(websocket.TextMessage, data)
					c.writeMu.Unlock()
				default:
					return
				}
			}
		default:
		}

		select {
		case data, ok := <-c.sendCh:
			if !ok {
				return
			}
			c.writeMu.Lock()
			_ = c.conn.WriteMessage(websocket.TextMessage, data)
			c.writeMu.Unlock()
		case <-c.doneCh:
			// drain 剩余消息后退出
			for {
				select {
				case data := <-c.sendCh:
					c.writeMu.Lock()
					_ = c.conn.WriteMessage(websocket.TextMessage, data)
					c.writeMu.Unlock()
				default:
					return
				}
			}
		}
	}
}

// SendBufferUsage 返回当前发送缓冲区使用量（用于 metrics）。
// sync 模式下返回 (0, 0)。
func (c *Conn) SendBufferUsage() (used, capacity int) {
	if c.sendCh == nil {
		return 0, 0
	}
	used = len(c.sendCh)
	capacity = cap(c.sendCh)
	return
}

// Flush 等待 sendCh 中的所有消息发送完成。
func (c *Conn) Flush() {
	if c.sendCh == nil {
		return
	}
	for {
		select {
		case data, ok := <-c.sendCh:
			if !ok {
				return
			}
			c.writeMu.Lock()
			_ = c.conn.WriteMessage(websocket.TextMessage, data)
			c.writeMu.Unlock()
		default:
			return
		}
	}
}
