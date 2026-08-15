package websocketx

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/redis/go-redis/v9"
)

// BroadcastBus 跨 AP 广播总线。
type BroadcastBus interface {
	// Publish 向 Redis channel 发布广播消息，其他 AP 订阅后执行本地广播。
	// senderNodeID 用于防止发布者自己收到消息后重复广播。
	Publish(ctx context.Context, msg *CmdMessage, roomID string, except []string, senderNodeID string) error
	// Subscribe 订阅广播消息，收到后调用 handler 执行本地广播。
	Subscribe(ctx context.Context, handler BroadcastHandler) error
	// Close 关闭总线（取消订阅 goroutine）。
	Close() error
}

// BroadcastHandler 处理跨 AP 广播消息的回调。
type BroadcastHandler func(ctx context.Context, msg *CmdMessage, roomID string, except []string)

// redisBroadcastBus 基于 Redis Pub/Sub 实现的广播总线。
type redisBroadcastBus struct {
	rdb         *redis.Client
	group       string
	channelName string
	nodeID      string
	logger      *logx.Logger
	subCancel   context.CancelFunc
	subWg       sync.WaitGroup
	closeOnce   sync.Once
}

// NewRedisBroadcastBus 创建 Redis Pub/Sub 广播总线。
// group 用于隔离不同集群的广播 channel。
// nodeID 是当前 AP 节点的唯一标识，用于防止自己发出的广播被自己接收。
func NewRedisBroadcastBus(rdb *redis.Client, group, nodeID string) BroadcastBus {
	if rdb == nil {
		return &noopBroadcastBus{}
	}
	bus := &redisBroadcastBus{
		rdb:         rdb,
		group:       group,
		channelName: "broadcast:" + group,
		nodeID:      nodeID,
		logger:      logx.Default().Module("mux"),
	}
	return bus
}

// Publish 向 Redis channel 发布广播。
func (b *redisBroadcastBus) Publish(ctx context.Context, msg *CmdMessage, roomID string, except []string, senderNodeID string) error {
	payload := broadcastMessage{
		Cmd:          msg.Cmd,
		Body:         msg.Body,
		RoomID:       roomID,
		Except:       except,
		SenderNodeID: senderNodeID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := b.rdb.Publish(ctx, b.channelName, data).Err(); err != nil {
		b.logger.Error("broadcast bus publish failed", logx.Fields{
			"error":   err,
			"channel": b.channelName,
			"cmd":     msg.Cmd,
		})
		return err
	}
	return nil
}

// Subscribe 订阅广播 channel。
func (b *redisBroadcastBus) Subscribe(ctx context.Context, handler BroadcastHandler) error {
	pubsub := b.rdb.Subscribe(ctx, b.channelName)

	// Verify subscription
	_, err := pubsub.Receive(ctx)
	if err != nil {
		return err
	}

	subCtx, cancel := context.WithCancel(context.Background())
	b.subCancel = cancel

	b.subWg.Add(1)
	go func() {
		defer b.subWg.Done()
		ch := pubsub.Channel()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var bm broadcastMessage
				if err := json.Unmarshal([]byte(msg.Payload), &bm); err != nil {
					b.logger.Warn("broadcast bus unmarshal failed", logx.Fields{"error": err})
					continue
				}
				// 跳过自己发出的广播
				if bm.SenderNodeID == b.nodeID {
					continue
				}
				handler(subCtx, &CmdMessage{
					Cmd:  bm.Cmd,
					Body: bm.Body,
				}, bm.RoomID, bm.Except)
			case <-subCtx.Done():
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	b.logger.Info("broadcast bus subscribed", logx.Fields{"channel": b.channelName, "node_id": b.nodeID})
	return nil
}

// Close 关闭广播总线。
func (b *redisBroadcastBus) Close() error {
	var err error
	b.closeOnce.Do(func() {
		if b.subCancel != nil {
			b.subCancel()
		}
		b.subWg.Wait()
	})
	return err
}

// broadcastMessage 跨 AP 广播的 wire 格式。
type broadcastMessage struct {
	Cmd          string   `json:"cmd"`
	Body         []byte   `json:"body"`
	RoomID       string   `json:"roomID,omitempty"`
	Except       []string `json:"except,omitempty"`
	SenderNodeID string   `json:"senderNodeID"`
}

// noopBroadcastBus 空实现的广播总线，用于无需跨 AP 广播的场景。
type noopBroadcastBus struct{}

func (n *noopBroadcastBus) Publish(ctx context.Context, msg *CmdMessage, roomID string, except []string, senderNodeID string) error {
	return nil
}
func (n *noopBroadcastBus) Subscribe(ctx context.Context, handler BroadcastHandler) error {
	return nil
}
func (n *noopBroadcastBus) Close() error {
	return nil
}
