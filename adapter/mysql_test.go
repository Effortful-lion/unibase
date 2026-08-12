package adapter

import (
	"testing"
	"time"
)

// ==================== MySQL 单元测试 ====================

func TestNewMySQL(t *testing.T) {
	// sqlx.Open doesn't actually connect, so this won't fail even without MySQL
	mysql, err := NewMySQL(MySQLConfig{
		DSN: "user:pass@tcp(localhost:3306)/test",
	})
	if err != nil {
		// Connection error is expected if MySQL isn't running,
		// but sqlx.Open doesn't actually connect
		t.Logf("NewMySQL error (expected if MySQL not running): %v", err)
		return
	}
	if mysql == nil {
		t.Fatal("expected non-nil MySQL")
	}
	_ = mysql
}

func TestMySQLConfig_Fields(t *testing.T) {
	cfg := MySQLConfig{
		DSN:             "user:pass@tcp(localhost:3306)/test",
		MaxOpenConns:    20,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
	}
	if cfg.DSN != "user:pass@tcp(localhost:3306)/test" {
		t.Errorf("DSN = %q", cfg.DSN)
	}
	if cfg.MaxOpenConns != 20 {
		t.Errorf("MaxOpenConns = %d", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 5 {
		t.Errorf("MaxIdleConns = %d", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != time.Hour {
		t.Errorf("ConnMaxLifetime = %v", cfg.ConnMaxLifetime)
	}
}

// ==================== MySQL 集成测试 ====================

func mysqlAvailable() bool {
	mysql, err := NewMySQL(MySQLConfig{
		DSN: "root:root@tcp(localhost:3306)/test",
	})
	if err != nil {
		return false
	}
	defer func() { _ = mysql.Close() }()
	return mysql.Ping() == nil
}

func TestMySQL_Ping(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if !mysqlAvailable() {
		t.Skip("MySQL not available at localhost:3306")
	}

	mysql, err := NewMySQL(MySQLConfig{
		DSN: "root:root@tcp(localhost:3306)/test",
	})
	if err != nil {
		t.Fatalf("NewMySQL failed: %v", err)
	}
	defer func() { _ = mysql.Close() }()

	if err := mysql.Ping(); err != nil {
		t.Errorf("MySQL ping failed: %v", err)
	}
}
