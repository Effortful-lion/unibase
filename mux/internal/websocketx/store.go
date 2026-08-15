package websocketx

import (
	"context"
	"time"
)

// MessageStore 消息持久化接口。
// 用于 IM 场景的离线消息存储：用户不在线时暂存，上线后拉取。
// 框架不自动调用，由业务 Handler 按需使用。
type MessageStore interface {
	// Save 存储一条离线消息。
	Save(ctx context.Context, msg *StoredMessage) error
	// FetchOffline 拉取用户的离线消息（按时间正序，最早的消息在前）。
	// limit <= 0 时拉取全部。
	FetchOffline(ctx context.Context, userID string, limit int) ([]*StoredMessage, error)
	// Ack 确认消息已送达，从离线队列移除指定消息。
	Ack(ctx context.Context, userID string, messageIDs []string) error
}

// StoredMessage 持久化消息结构。
type StoredMessage struct {
	ID         string    `json:"id"`
	FromUserID string    `json:"from_user_id"`
	ToUserID   string    `json:"to_user_id"`
	Cmd        string    `json:"cmd"`
	Body       []byte    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}
