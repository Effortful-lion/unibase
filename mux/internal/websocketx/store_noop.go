package websocketx

import "context"

// NoOpMessageStore 空实现的消息存储，所有操作均为 no-op。
// 用于不需要消息持久化的场景，作为默认实现。
type NoOpMessageStore struct{}

// NewNoOpMessageStore 创建空实现的消息存储。
func NewNoOpMessageStore() MessageStore {
	return &NoOpMessageStore{}
}

func (n *NoOpMessageStore) Save(ctx context.Context, msg *StoredMessage) error {
	return nil
}

func (n *NoOpMessageStore) FetchOffline(ctx context.Context, userID string, limit int) ([]*StoredMessage, error) {
	return nil, nil
}

func (n *NoOpMessageStore) Ack(ctx context.Context, userID string, messageIDs []string) error {
	return nil
}
