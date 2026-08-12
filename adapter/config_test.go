package adapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnmarshalConfig_Complete(t *testing.T) {
	data := map[string]any{
		"redis": map[string]any{
			"addr":     "localhost:6379",
			"password": "secret",
			"db":       1,
		},
		"mysql": map[string]any{
			"dsn": "user:pass@tcp(localhost:3306)/db",
		},
	}

	cfg, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Redis == nil {
		t.Fatal("expected Redis config to be set")
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("Redis.Addr = %q, want %q", cfg.Redis.Addr, "localhost:6379")
	}
	if cfg.Redis.Password != "secret" {
		t.Errorf("Redis.Password = %q, want %q", cfg.Redis.Password, "secret")
	}
	if cfg.Redis.DB != 1 {
		t.Errorf("Redis.DB = %d, want %d", cfg.Redis.DB, 1)
	}

	if cfg.MySQL == nil {
		t.Fatal("expected MySQL config to be set")
	}
	if cfg.MySQL.DSN != "user:pass@tcp(localhost:3306)/db" {
		t.Errorf("MySQL.DSN = %q", cfg.MySQL.DSN)
	}
}

func TestUnmarshalConfig_Partial(t *testing.T) {
	// Only Redis configured, others should be nil
	data := map[string]any{
		"redis": map[string]any{
			"addr": "localhost:6379",
		},
	}

	cfg, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Redis == nil {
		t.Fatal("expected Redis config to be set")
	}
	if cfg.MySQL != nil {
		t.Error("expected MySQL config to be nil")
	}
	if cfg.ES != nil {
		t.Error("expected ES config to be nil")
	}
	if cfg.Kafka != nil {
		t.Error("expected Kafka config to be nil")
	}
	if cfg.Mongo != nil {
		t.Error("expected Mongo config to be nil")
	}
}

func TestUnmarshalConfig_Empty(t *testing.T) {
	cfg, err := UnmarshalConfig(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Redis != nil {
		t.Error("expected Redis config to be nil")
	}
	if cfg.MySQL != nil {
		t.Error("expected MySQL config to be nil")
	}
}

func TestUnmarshalConfig_MalformedJSON(t *testing.T) {
	// When JSON types don't match struct fields, json.Unmarshal returns an error.
	// This verifies that UnmarshalConfig propagates the error correctly.
	data := map[string]any{
		"redis": map[string]any{
			"db": "not_an_int", // type mismatch: string vs int
		},
	}

	_, err := UnmarshalConfig(data)
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected unmarshal error, got: %v", err)
	}
}

func TestUnmarshalConfig_NestedTypes(t *testing.T) {
	// Without struct tags, json.Unmarshal uses exact field name matching (case-insensitive).
	// JSON keys must match Go field names: ClientID (not client_id), ListenAddr (not listen_addr)
	data := map[string]any{
		"kafka": map[string]any{
			"Brokers":  []string{"k1:9092", "k2:9092"},
			"ClientID": "my-app",
		},
		"prometheus": map[string]any{
			"Namespace":  "myapp",
			"Subsystem":  "http",
			"ListenAddr": ":9090",
		},
		"es": map[string]any{
			"Addresses": []string{"http://es1:9200", "http://es2:9200"},
			"Username":  "elastic",
			"Password":  "secret",
		},
		"mongo": map[string]any{
			"URI": "mongodb://mongo:27017",
		},
	}

	cfg, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Kafka == nil {
		t.Fatal("expected Kafka config")
	}
	if len(cfg.Kafka.Brokers) != 2 {
		t.Errorf("Kafka.Brokers len = %d, want 2", len(cfg.Kafka.Brokers))
	}
	if cfg.Kafka.ClientID != "my-app" {
		t.Errorf("Kafka.ClientID = %q", cfg.Kafka.ClientID)
	}

	if cfg.Prometheus == nil {
		t.Fatal("expected Prometheus config")
	}
	if cfg.Prometheus.Namespace != "myapp" {
		t.Errorf("Prometheus.Namespace = %q", cfg.Prometheus.Namespace)
	}

	if cfg.ES == nil {
		t.Fatal("expected ES config")
	}
	if len(cfg.ES.Addresses) != 2 {
		t.Errorf("ES.Addresses len = %d, want 2", len(cfg.ES.Addresses))
	}

	if cfg.Mongo == nil {
		t.Fatal("expected Mongo config")
	}
	if cfg.Mongo.URI != "mongodb://mongo:27017" {
		t.Errorf("Mongo.URI = %q", cfg.Mongo.URI)
	}
}

func TestUnmarshalConfig_MatchesJSON(t *testing.T) {
	// Verify that UnmarshalConfig produces the same result as json.Unmarshal.
	// JSON keys must match Go field names (no struct tags in Config types).
	jsonBlob := []byte(`{
		"Redis": {"Addr": "localhost:6379", "DB": 2, "MaxRetry": 5},
		"MySQL": {"DSN": "test:test@tcp(localhost:3306)/test"}
	}`)

	var direct Config
	if err := json.Unmarshal(jsonBlob, &direct); err != nil {
		t.Fatalf("direct json unmarshal failed: %v", err)
	}

	data := map[string]any{}
	if err := json.Unmarshal(jsonBlob, &data); err != nil {
		t.Fatalf("map unmarshal failed: %v", err)
	}

	indirect, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("UnmarshalConfig failed: %v", err)
	}

	if direct.Redis == nil || indirect.Redis == nil {
		t.Fatal("both Redis configs should be set")
	}
	if direct.Redis.Addr != indirect.Redis.Addr {
		t.Errorf("Redis.Addr mismatch: direct=%q indirect=%q", direct.Redis.Addr, indirect.Redis.Addr)
	}
	if direct.Redis.DB != indirect.Redis.DB {
		t.Errorf("Redis.DB mismatch: direct=%d indirect=%d", direct.Redis.DB, indirect.Redis.DB)
	}
	if direct.Redis.MaxRetry != indirect.Redis.MaxRetry {
		t.Errorf("Redis.MaxRetry mismatch: direct=%d indirect=%d", direct.Redis.MaxRetry, indirect.Redis.MaxRetry)
	}
	if direct.MySQL.DSN != indirect.MySQL.DSN {
		t.Errorf("MySQL.DSN mismatch: direct=%q indirect=%q", direct.MySQL.DSN, indirect.MySQL.DSN)
	}
}
