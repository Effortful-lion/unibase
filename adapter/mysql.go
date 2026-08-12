package adapter

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

// MySQLConfig MySQL 连接配置。
type MySQLConfig struct {
	// DSN 数据源名称，必填。
	// 格式：username:password@tcp(host:port)/database?param=value
	DSN string

	// MaxOpenConns 最大打开连接数，0 表示使用默认值。
	MaxOpenConns int

	// MaxIdleConns 最大空闲连接数，0 表示使用默认值。
	MaxIdleConns int

	// ConnMaxLifetime 连接最大生命周期，0 表示使用默认值。
	ConnMaxLifetime time.Duration
}

// MySQL 是 MySQL 客户端的薄封装。
// 持有标准库的 *sqlx.DB，核心能力直接委托给原始客户端。
type MySQL struct {
	db *sqlx.DB
}

// NewMySQL 创建 MySQL 适配器。
func NewMySQL(cfg MySQLConfig) (*MySQL, error) {
	db, err := sqlx.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, err
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}

	return &MySQL{db: db}, nil
}

// DB 返回底层的 *sqlx.DB，可直接使用 sqlx 的全部能力。
func (m *MySQL) DB() *sqlx.DB { return m.db }

// SQL 返回底层的 *sql.DB，用于标准库 database/sql 操作。
func (m *MySQL) SQL() *sql.DB { return m.db.DB }

// Ping 检查 MySQL 连接是否可用。
func (m *MySQL) Ping() error {
	return m.db.Ping()
}

// Close 关闭 MySQL 连接池。
func (m *MySQL) Close() error {
	return m.db.Close()
}

// ==================== 快捷操作 ====================

// Exec 执行非查询 SQL（INSERT / UPDATE / DELETE）。
// result 可调用 LastInsertId() / RowsAffected() 获取结果。
func (m *MySQL) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return m.db.ExecContext(ctx, query, args...)
}

// NamedExec 使用命名参数执行 SQL。
// args 支持 map[string]any 或 struct。
func (m *MySQL) NamedExec(ctx context.Context, query string, args any) (sql.Result, error) {
	return m.db.NamedExecContext(ctx, query, args)
}

// Query 执行查询并将结果扫描到 dest 切片中。
// dest 必须为指针切片，如 &[]User{}。
func (m *MySQL) Query(ctx context.Context, dest any, query string, args ...any) error {
	return m.db.SelectContext(ctx, dest, query, args...)
}

// Get 执行查询并扫描单行结果到 dest。
// dest 必须为指针，如 &user。
func (m *MySQL) Get(ctx context.Context, dest any, query string, args ...any) error {
	return m.db.GetContext(ctx, dest, query, args...)
}
