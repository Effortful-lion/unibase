package websocketx

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/redis/go-redis/v9"
)

// RedisMessageStore 基于 Redis 的离线消息存储。
// 使用 ZSET 维护消息时间顺序，Hash 存储消息内容：
//   - ZSET: offline:{userID} → score=timestamp(纳秒), member=messageID
//   - Hash: offline_msg:{userID} → field=messageID, value=JSON
type RedisMessageStore struct {
	rdb    *redis.Client
	logger *logx.Logger
}

// NewRedisMessageStore 创建 Redis 实现的消息存储。
// rdb 为 nil 时返回 NoOp 实现。
func NewRedisMessageStore(rdb *redis.Client) MessageStore {
	if rdb == nil {
		return &NoOpMessageStore{}
	}
	return &RedisMessageStore{
		rdb:    rdb,
		logger: logx.Default().Module("mux"),
	}
}

// Save 存储一条离线消息。
func (s *RedisMessageStore) Save(ctx context.Context, msg *StoredMessage) error {
	if msg == nil || msg.ToUserID == "" {
		return fmt.Errorf("websocketx: invalid stored message")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("websocketx: marshal stored message: %w", err)
	}

	zsetKey := offlineZSetKey(msg.ToUserID)
	hashKey := offlineHashKey(msg.ToUserID)
	score := float64(msg.CreatedAt.UnixNano())

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	pipe := s.rdb.Pipeline()
	pipe.ZAdd(ctx, zsetKey, redis.Z{Score: score, Member: msg.ID})
	pipe.HSet(ctx, hashKey, msg.ID, data)
	_, err = pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("message store save failed", logx.Fields{
			"error":   err,
			"to_user": msg.ToUserID,
			"msg_id":  msg.ID,
		})
		return err
	}
	return nil
}

// FetchOffline 拉取用户的离线消息（按时间正序）。
// limit <= 0 时拉取全部。
func (s *RedisMessageStore) FetchOffline(ctx context.Context, userID string, limit int) ([]*StoredMessage, error) {
	zsetKey := offlineZSetKey(userID)
	hashKey := offlineHashKey(userID)

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	start := int64(0)
	stop := int64(-1)
	if limit > 0 {
		stop = int64(limit - 1)
	}

	msgIDs, err := s.rdb.ZRange(ctx, zsetKey, start, stop).Result()
	if err != nil {
		s.logger.Error("message store fetch zrange failed", logx.Fields{
			"error":   err,
			"user_id": userID,
		})
		return nil, err
	}

	if len(msgIDs) == 0 {
		return nil, nil
	}

	values, err := s.rdb.HMGet(ctx, hashKey, msgIDs...).Result()
	if err != nil {
		s.logger.Error("message store fetch hmget failed", logx.Fields{
			"error":   err,
			"user_id": userID,
		})
		return nil, err
	}

	messages := make([]*StoredMessage, 0, len(values))
	for _, v := range values {
		str, ok := v.(string)
		if !ok {
			continue
		}
		var msg StoredMessage
		if err := json.Unmarshal([]byte(str), &msg); err != nil {
			continue
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

// Ack 确认消息已送达，从离线队列移除。
func (s *RedisMessageStore) Ack(ctx context.Context, userID string, messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}

	zsetKey := offlineZSetKey(userID)
	hashKey := offlineHashKey(userID)

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	members := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		members[i] = id
	}

	pipe := s.rdb.Pipeline()
	pipe.ZRem(ctx, zsetKey, members...)
	pipe.HDel(ctx, hashKey, messageIDs...)
	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("message store ack failed", logx.Fields{
			"error":   err,
			"user_id": userID,
		})
		return err
	}
	return nil
}

func offlineZSetKey(userID string) string {
	return "offline:" + userID
}

func offlineHashKey(userID string) string {
	return "offline_msg:" + userID
}
