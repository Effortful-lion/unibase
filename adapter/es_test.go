package adapter

import (
	"testing"
)

// ==================== ES 单元测试 ====================

func TestNewES(t *testing.T) {
	es, err := NewES(ESConfig{
		Addresses: []string{"http://localhost:9200"},
	})
	if err != nil {
		// Connection error is expected if ES isn't running
		t.Logf("NewES error (expected if ES not running): %v", err)
		return
	}
	if es == nil {
		t.Fatal("expected non-nil ES")
	}
	_ = es
}

func TestESConfig_Fields(t *testing.T) {
	cfg := ESConfig{
		Addresses: []string{"http://es1:9200", "http://es2:9200"},
		Username:  "elastic",
		Password:  "secret",
	}
	if len(cfg.Addresses) != 2 {
		t.Errorf("Addresses len = %d, want 2", len(cfg.Addresses))
	}
	if cfg.Username != "elastic" {
		t.Errorf("Username = %q", cfg.Username)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password = %q", cfg.Password)
	}
}

// ==================== ES 集成测试 ====================

func esAvailable() bool {
	es, err := NewES(ESConfig{Addresses: []string{"http://localhost:9200"}})
	if err != nil {
		return false
	}
	defer func() { _ = es.Close() }()
	return es.Ping() == nil
}

func TestES_Ping(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if !esAvailable() {
		t.Skip("ES not available at localhost:9200")
	}

	es, err := NewES(ESConfig{Addresses: []string{"http://localhost:9200"}})
	if err != nil {
		t.Fatalf("NewES failed: %v", err)
	}
	defer func() { _ = es.Close() }()

	if err := es.Ping(); err != nil {
		t.Errorf("ES ping failed: %v", err)
	}
}
