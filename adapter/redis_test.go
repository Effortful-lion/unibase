package adapter

import (
	"context"
	"testing"
	"time"
)

// ==================== Redis 单元测试 ====================

func TestNewRedis(t *testing.T) {
	r := NewRedis(RedisConfig{
		Addr: "localhost:6379",
	})
	if r == nil {
		t.Fatal("expected non-nil Redis")
	}
	if r.client == nil {
		t.Error("expected non-nil redis client")
	}
}

func TestNewRedis_Defaults(t *testing.T) {
	r := NewRedis(RedisConfig{
		Addr:     "localhost:6379",
		Password: "secret",
		DB:       2,
	})
	_ = r
	// Just verify it doesn't panic; the options are set internally
}

func TestRedis_Client(t *testing.T) {
	r := NewRedis(RedisConfig{Addr: "localhost:6379"})
	client := r.Client()
	if client == nil {
		t.Fatal("expected non-nil redis client")
	}
}

// ==================== Redis 集成测试 ====================

func redisAvailable() bool {
	// Check if Redis is running locally
	r := NewRedis(RedisConfig{Addr: "localhost:6379"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.Ping(ctx) == nil
}

func TestRedis_Ping(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if !redisAvailable() {
		t.Skip("Redis not available at localhost:6379")
	}

	r := NewRedis(RedisConfig{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := r.Ping(ctx); err != nil {
		t.Errorf("Redis ping failed: %v", err)
	}
}

func TestRedis_SetGetDel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if !redisAvailable() {
		t.Skip("Redis not available at localhost:6379")
	}

	r := NewRedis(RedisConfig{Addr: "localhost:6379", DB: 15}) // use test DB
	ctx := context.Background()
	key := "adapter_test_key"

	// Cleanup
	_ = r.Del(ctx, key)

	// Set
	if err := r.Set(ctx, key, "hello", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get
	val, err := r.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "hello" {
		t.Errorf("Get = %q, want %q", val, "hello")
	}

	// Exists
	count, err := r.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Exists = %d, want 1", count)
	}

	// Del
	if err := r.Del(ctx, key); err != nil {
		t.Fatalf("Del failed: %v", err)
	}

	// Get after del should be empty
	val, err = r.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after del failed: %v", err)
	}
	if val != "" {
		t.Errorf("Get after del = %q, want empty", val)
	}
}

func TestRedis_MGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if !redisAvailable() {
		t.Skip("Redis not available at localhost:6379")
	}

	r := NewRedis(RedisConfig{Addr: "localhost:6379", DB: 15})
	ctx := context.Background()

	keys := []string{"k1", "k2", "k3"}
	_ = r.Del(ctx, keys...)
	_ = r.Set(ctx, "k1", "v1", 0)
	_ = r.Set(ctx, "k2", "v2", 0)
	// k3 intentionally missing

	vals, err := r.MGet(ctx, keys...)
	if err != nil {
		t.Fatalf("MGet failed: %v", err)
	}
	if len(vals) != 3 {
		t.Fatalf("MGet len = %d, want 3", len(vals))
	}
	if vals[0] != "v1" {
		t.Errorf("MGet[0] = %v, want v1", vals[0])
	}
	if vals[1] != "v2" {
		t.Errorf("MGet[1] = %v, want v2", vals[1])
	}
	if vals[2] != nil {
		t.Errorf("MGet[2] = %v, want nil", vals[2])
	}
}

func TestRedis_IncrDecr(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if !redisAvailable() {
		t.Skip("Redis not available at localhost:6379")
	}

	r := NewRedis(RedisConfig{Addr: "localhost:6379", DB: 15})
	ctx := context.Background()
	key := "adapter_test_counter"

	_ = r.Del(ctx, key)

	n, err := r.Incr(ctx, key)
	if err != nil {
		t.Fatalf("Incr failed: %v", err)
	}
	if n != 1 {
		t.Errorf("Incr = %d, want 1", n)
	}

	n, err = r.Incr(ctx, key)
	if err != nil {
		t.Fatalf("Incr failed: %v", err)
	}
	if n != 2 {
		t.Errorf("Incr = %d, want 2", n)
	}

	n, err = r.Decr(ctx, key)
	if err != nil {
		t.Fatalf("Decr failed: %v", err)
	}
	if n != 1 {
		t.Errorf("Decr = %d, want 1", n)
	}
}

func TestRedis_SetEX(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if !redisAvailable() {
		t.Skip("Redis not available at localhost:6379")
	}

	r := NewRedis(RedisConfig{Addr: "localhost:6379", DB: 15})
	ctx := context.Background()
	key := "adapter_test_ex"

	_ = r.Del(ctx, key)

	if err := r.SetEX(ctx, key, "temp", 3600); err != nil {
		t.Fatalf("SetEX failed: %v", err)
	}

	val, err := r.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "temp" {
		t.Errorf("Get = %q, want %q", val, "temp")
	}

	// TTL should be ~3600 seconds
	ttl, err := r.TTL(ctx, key)
	if err != nil {
		t.Fatalf("TTL failed: %v", err)
	}
	if ttl <= 0 || ttl > 3601*time.Second {
		t.Errorf("TTL = %v, expected ~1h", ttl)
	}
}
