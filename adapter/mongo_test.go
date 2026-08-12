package adapter

import (
	"testing"
)

// ==================== Mongo 单元测试 ====================

func TestNewMongo(t *testing.T) {
	m, err := NewMongo(MongoConfig{
		URI: "mongodb://localhost:27017",
	})
	if err != nil {
		// Connection error is expected if Mongo isn't running
		t.Logf("NewMongo error (expected if Mongo not running): %v", err)
		return
	}
	if m == nil {
		t.Fatal("expected non-nil Mongo")
	}
	defer func() { _ = m.Close() }()
}

func TestMongoConfig_Fields(t *testing.T) {
	cfg := MongoConfig{
		URI: "mongodb://user:pass@mongo1:27017,mongo2:27017/?replicaSet=rs0",
	}
	if cfg.URI != "mongodb://user:pass@mongo1:27017,mongo2:27017/?replicaSet=rs0" {
		t.Errorf("URI = %q", cfg.URI)
	}
}

// ==================== Mongo 集成测试 ====================

func mongoAvailable() bool {
	m, err := NewMongo(MongoConfig{URI: "mongodb://localhost:27017"})
	if err != nil {
		return false
	}
	defer func() { _ = m.Close() }()
	// Ping is implicit in NewMongo, but we can verify by accessing the client
	return m.Client() != nil
}

func TestMongo_Client(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if !mongoAvailable() {
		t.Skip("Mongo not available at localhost:27017")
	}

	m, err := NewMongo(MongoConfig{URI: "mongodb://localhost:27017"})
	if err != nil {
		t.Fatalf("NewMongo failed: %v", err)
	}
	defer func() { _ = m.Close() }()

	if m.Client() == nil {
		t.Error("expected non-nil mongo client")
	}

	db := m.Database("testdb")
	if db == nil {
		t.Error("expected non-nil database")
	}

	coll := m.Collection("testdb", "testcoll")
	if coll == nil {
		t.Error("expected non-nil collection")
	}
}
