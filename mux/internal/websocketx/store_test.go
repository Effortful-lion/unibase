package websocketx

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ── NoOpMessageStore ────────────────────────────────────────

func TestNoOpMessageStore_Save(t *testing.T) {
	store := NewNoOpMessageStore()
	err := store.Save(context.Background(), &StoredMessage{
		ID:         "msg_1",
		FromUserID: "user_A",
		ToUserID:   "user_B",
		Cmd:        "chat.message",
		Body:       []byte(`{"text":"hello"}`),
		CreatedAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestNoOpMessageStore_FetchOffline_ReturnsNil(t *testing.T) {
	store := NewNoOpMessageStore()
	msgs, err := store.FetchOffline(context.Background(), "user_B", 10)
	if err != nil {
		t.Fatalf("FetchOffline: %v", err)
	}
	if msgs != nil {
		t.Errorf("expected nil, got %v", msgs)
	}
}

func TestNoOpMessageStore_Ack(t *testing.T) {
	store := NewNoOpMessageStore()
	err := store.Ack(context.Background(), "user_B", []string{"msg_1"})
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
}

// ── RedisMessageStore 集成测试 ──────────────────────────────
// 本地运行：REDIS_ADDR=localhost:6379 go test ./internal/websocketx/ -run TestRedisMessageStore
// CI 中：go test -short ./internal/websocketx/ （跳过集成测试）

func TestRedisMessageStore_SaveFetchAck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rdb, cleanup := newRedisClientForStoreTest(t)
	defer cleanup()

	store := NewRedisMessageStore(rdb)
	ctx := context.Background()
	userID := "test_user_store"

	// 清理可能的残留数据
	rdb.Del(ctx, offlineZSetKey(userID), offlineHashKey(userID))

	// Save 3 条消息
	msgs := []*StoredMessage{
		{ID: "msg_1", FromUserID: "user_A", ToUserID: userID, Cmd: "chat.message", Body: []byte(`{"text":"hello"}`), CreatedAt: time.Now().Add(-2 * time.Second)},
		{ID: "msg_2", FromUserID: "user_A", ToUserID: userID, Cmd: "chat.message", Body: []byte(`{"text":"world"}`), CreatedAt: time.Now().Add(-1 * time.Second)},
		{ID: "msg_3", FromUserID: "user_C", ToUserID: userID, Cmd: "chat.message", Body: []byte(`{"text":"foo"}`), CreatedAt: time.Now()},
	}
	for _, msg := range msgs {
		if err := store.Save(ctx, msg); err != nil {
			t.Fatalf("Save %s: %v", msg.ID, err)
		}
	}

	// FetchOffline：应返回 3 条，按时间正序
	fetched, err := store.FetchOffline(ctx, userID, 0)
	if err != nil {
		t.Fatalf("FetchOffline: %v", err)
	}
	if len(fetched) != 3 {
		t.Fatalf("fetched count: got %d, want 3", len(fetched))
	}
	if fetched[0].ID != "msg_1" {
		t.Errorf("first message ID: got %s, want msg_1", fetched[0].ID)
	}

	// FetchOffline with limit=2
	fetchedLimited, err := store.FetchOffline(ctx, userID, 2)
	if err != nil {
		t.Fatalf("FetchOffline limited: %v", err)
	}
	if len(fetchedLimited) != 2 {
		t.Fatalf("fetched limited count: got %d, want 2", len(fetchedLimited))
	}

	// Ack msg_1 和 msg_2
	if err := store.Ack(ctx, userID, []string{"msg_1", "msg_2"}); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	// FetchOffline：应只剩 msg_3
	remaining, err := store.FetchOffline(ctx, userID, 0)
	if err != nil {
		t.Fatalf("FetchOffline after ack: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("remaining count: got %d, want 1", len(remaining))
	}
	if remaining[0].ID != "msg_3" {
		t.Errorf("remaining message ID: got %s, want msg_3", remaining[0].ID)
	}

	// 清理
	rdb.Del(ctx, offlineZSetKey(userID), offlineHashKey(userID))
}

func TestRedisMessageStore_FetchOffline_Empty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rdb, cleanup := newRedisClientForStoreTest(t)
	defer cleanup()

	store := NewRedisMessageStore(rdb)
	ctx := context.Background()

	fetched, err := store.FetchOffline(ctx, "nonexistent_user", 10)
	if err != nil {
		t.Fatalf("FetchOffline: %v", err)
	}
	if fetched != nil {
		t.Errorf("expected nil for nonexistent user, got %v", fetched)
	}
}

func TestRedisMessageStore_NilRdb_ReturnsNoOp(t *testing.T) {
	store := NewRedisMessageStore(nil)
	if _, ok := store.(*NoOpMessageStore); !ok {
		t.Error("expected NoOpMessageStore for nil rdb")
	}
}

// ── Redis 测试辅助 ──────────────────────────────────────────

func newRedisClientForStoreTest(t *testing.T) (*redis.Client, func()) {
	t.Helper()

	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: addr})
		return rdb, func() { rdb.Close() }
	}

	// 使用 testcontainer
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("skipping: cannot start redis container: %v", err)
	}

	host, _ := container.Host(ctx)
	port, _ := container.MappedPort(ctx, "6379")
	addr := host + ":" + port.Port()

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	return rdb, func() {
		rdb.Close()
		container.Terminate(ctx)
	}
}
