package adapter

import (
	"context"
	"testing"
	"time"
)

// ==================== Kafka 单元测试 ====================

func TestNewKafka(t *testing.T) {
	k, err := NewKafka(KafkaConfig{
		Brokers:  []string{"localhost:9092"},
		ClientID: "test",
	})
	if err != nil {
		// Connection error is expected if Kafka isn't running
		t.Logf("NewKafka error (expected if Kafka not running): %v", err)
		return
	}
	if k == nil {
		t.Fatal("expected non-nil Kafka")
	}
	defer func() { _ = k.Close() }()
}

func TestKafkaConfig_Fields(t *testing.T) {
	cfg := KafkaConfig{
		Brokers:  []string{"k1:9092", "k2:9092"},
		ClientID: "my-app",
	}
	if len(cfg.Brokers) != 2 {
		t.Errorf("Brokers len = %d, want 2", len(cfg.Brokers))
	}
	if cfg.ClientID != "my-app" {
		t.Errorf("ClientID = %q", cfg.ClientID)
	}
}

// ==================== Kafka 集成测试 ====================

func kafkaAvailable() bool {
	k, err := NewKafka(KafkaConfig{Brokers: []string{"localhost:9092"}})
	if err != nil {
		return false
	}
	defer func() { _ = k.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return k.Ping(ctx) == nil
}

func TestKafka_Ping(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if !kafkaAvailable() {
		t.Skip("Kafka not available at localhost:9092")
	}

	k, err := NewKafka(KafkaConfig{Brokers: []string{"localhost:9092"}})
	if err != nil {
		t.Fatalf("NewKafka failed: %v", err)
	}
	defer func() { _ = k.Close() }()

	ctx := context.Background()
	if err := k.Ping(ctx); err != nil {
		t.Errorf("Kafka ping failed: %v", err)
	}
}
