package websocketx

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ── 心跳 ──────────────────────────────────────────────────────

// Heartbeat 管理单条连接的 Ping/Pong 保活。
type Heartbeat struct {
	session   *Session
	interval  time.Duration
	pongWait  time.Duration
	stopCh    chan struct{}
	stopped   chan struct{}
	closeOnce sync.Once
}

// StartHeartbeat 启动心跳保活。
// 内部起两个 goroutine：ticker 定时发 Ping，pongWaiter 等待 Pong 超时则断开。
// 调用 Stop() 或 Session 关闭时自动清理。
func StartHeartbeat(ctx context.Context, session *Session, interval, pongWait time.Duration) *Heartbeat {
	hb := &Heartbeat{
		session:  session,
		interval: interval,
		pongWait: pongWait,
		stopCh:   make(chan struct{}),
		stopped:  make(chan struct{}),
	}

	// 注册 Pong handler，更新最后收到 Pong 的时间
	_ = session.Conn().SetPongHandler(func(_ string) error {
		session.updatePongTime()
		return nil
	})

	// 定时发送 Ping
	go func() {
		defer hb.closeStopped()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := session.Conn().Ping(ctx); err != nil {
					return
				}
			case <-hb.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Pong 超时检测
	go func() {
		defer hb.closeStopped()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if session.pingTimeout(hb.pongWait) {
					_ = session.Conn().Close(
						int(websocket.CloseGoingAway),
						string(CloseReasonHeartbeatTimeout),
					)
					return
				}
			case <-hb.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return hb
}

// closeStopped 安全关闭 stopped channel（确保只关闭一次）。
func (hb *Heartbeat) closeStopped() {
	hb.closeOnce.Do(func() { close(hb.stopped) })
}

// Stop 停止心跳，等待两个内部 goroutine 退出。
func (hb *Heartbeat) Stop() {
	select {
	case hb.stopCh <- struct{}{}:
	default:
	}
	<-hb.stopped
}
