package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Effortful-lion/unibase/logx"
)

// Config 是所有第三方服务的统一配置聚合。
// 只有非 nil 的服务才会被初始化。
//
// 注意：Config 本身不含任何序列化标签，
// 它纯粹是初始化用的数据结构，不强制配置文件格式。
// 用户根据实际配置文件结构自行映射填充。
type Config struct {
	Redis      *RedisConfig
	MySQL      *MySQLConfig
	ES         *ESConfig
	Kafka      *KafkaConfig
	Prometheus *PrometheusConfig
	Mongo      *MongoConfig
}

// Client 聚合所有已初始化的第三方服务客户端。
// 每个字段在对应服务未配置时为 nil。
type Client struct {
	redis      *Redis
	mysql      *MySQL
	es         *ES
	kafka      *Kafka
	prometheus *Prometheus
	mongo      *Mongo
	logger     *logx.Logger
}

// New 从配置初始化所有已配置的第三方服务。
// 如果某个服务的 Config 为 nil，则跳过该服务的初始化，对应 Client 字段为 nil。
// 返回的 err 只包含初始化过程中的致命错误（如网络不可达）。
// 部分服务初始化失败不会影响其他服务的初始化。
func New(cfg Config, opts ...Option) (*Client, error) {
	opt := defaultClientOptions()
	for _, o := range opts {
		o(&opt)
	}

	client := &Client{
		logger: opt.logger,
	}

	// 逐个初始化，互不影响
	if cfg.Redis != nil {
		client.redis = NewRedis(*cfg.Redis)
		if err := client.redis.Ping(context.Background()); err != nil {
			opt.logger.Warn("redis ping failed", logx.Fields{"err": err.Error()})
		}
	}

	if cfg.MySQL != nil {
		mysql, err := NewMySQL(*cfg.MySQL)
		if err != nil {
			opt.logger.Warn("mysql init failed", logx.Fields{"err": err.Error()})
		} else {
			client.mysql = mysql
		}
	}

	if cfg.ES != nil {
		es, err := NewES(*cfg.ES)
		if err != nil {
			opt.logger.Warn("es init failed", logx.Fields{"err": err.Error()})
		} else {
			client.es = es
		}
	}

	if cfg.Kafka != nil {
		kafka, err := NewKafka(*cfg.Kafka)
		if err != nil {
			opt.logger.Warn("kafka init failed", logx.Fields{"err": err.Error()})
		} else {
			client.kafka = kafka
		}
	}

	if cfg.Prometheus != nil {
		client.prometheus = NewPrometheus(*cfg.Prometheus)
	}

	if cfg.Mongo != nil {
		mongo, err := NewMongo(*cfg.Mongo)
		if err != nil {
			opt.logger.Warn("mongo init failed", logx.Fields{"err": err.Error()})
		} else {
			client.mongo = mongo
		}
	}

	return client, nil
}

// UnmarshalConfig 从 map[string]any 反序列化适配器配置。
// 这是一个便捷辅助，默认约定配置键名与结构体字段一致（小写）。
// 如果你的配置文件 key 路径不同（例如 "cache.redis.addr"），
// 请手动从 map 中取值构造 Config，不要依赖此方法。
//
// 默认约定映射：
//
//	redis.addr        → RedisConfig.Addr
//	redis.password    → RedisConfig.Password
//	redis.db          → RedisConfig.DB
//	mysql.dsn         → MySQLConfig.DSN
//	es.addresses      → ESConfig.Addresses
//	kafka.brokers     → KafkaConfig.Brokers
//	mongo.uri         → MongoConfig.URI
//
//	cfg, _ := configx.ReadConfig("config.yaml")
//	adapterCfg, err := adapter.UnmarshalConfig(cfg.All())
func UnmarshalConfig(data map[string]any) (*Config, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("adapter: marshal config failed: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(jsonBytes, &cfg); err != nil {
		return nil, fmt.Errorf("adapter: unmarshal config failed: %w", err)
	}

	return &cfg, nil
}
