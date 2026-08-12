package adapter

import (
	"context"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoConfig MongoDB 连接配置。
type MongoConfig struct {
	// URI 连接字符串，必填。
	// 例如 "mongodb://localhost:27017"
	URI string
}

// Mongo 是 MongoDB 客户端的薄封装。
// 持有标准库的 *mongo.Client，核心能力直接委托给原始客户端。
type Mongo struct {
	client *mongo.Client
	logger *logx.Logger
}

// NewMongo 创建 MongoDB 适配器。
func NewMongo(cfg MongoConfig) (*Mongo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(cfg.URI))
	if err != nil {
		return nil, err
	}

	// 验证连接
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}

	return &Mongo{
		client: client,
		logger: logx.Module("adapter.mongo"),
	}, nil
}

// Client 返回底层的 *mongo.Client，可直接使用 mongo-driver 的全部能力。
func (m *Mongo) Client() *mongo.Client { return m.client }

// Database 返回指定数据库的操作入口。
func (m *Mongo) Database(name string) *mongo.Database {
	return m.client.Database(name)
}

// Collection 返回指定集合的操作入口。
func (m *Mongo) Collection(db, coll string) *mongo.Collection {
	return m.client.Database(db).Collection(coll)
}

// Close 关闭 MongoDB 连接。
func (m *Mongo) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return m.client.Disconnect(ctx)
}
