# adapter

第三方服务客户端的薄初始化封装。每个服务独立可选，初始化失败互不影响。

## 目录结构

```
adapter/
├── doc.go          # 包说明
├── config.go       # Config / Client 统一入口
├── options.go      # 可选参数（注入 logger）
├── errors.go       # 领域错误定义
├── redis.go        # Redis 封装
├── mysql.go        # MySQL 封装
├── es.go           # Elasticsearch 封装
├── kafka.go        # Kafka 封装
├── mongo.go        # MongoDB 封装
├── prometheus.go   # Prometheus 封装
├── *_test.go       # 各服务的单元测试
├── go.mod
└── go.sum
```

## 支持的服务

| 服务 | 底层 SDK | 文件 | 类型 |
|------|----------|------|------|
| Redis | github.com/redis/go-redis/v9 | `redis.go` | 有状态 |
| MySQL | github.com/jmoiron/sqlx | `mysql.go` | 有状态 |
| Elasticsearch | github.com/elastic/go-elasticsearch/v8 | `es.go` | 有状态 |
| Kafka | github.com/twmb/franz-go | `kafka.go` | 有状态 |
| MongoDB | go.mongodb.org/mongo-driver/v2 | `mongo.go` | 有状态 |
| Prometheus | github.com/prometheus/client_golang | `prometheus.go` | 无状态 |

## 设计原则

1. **薄封装**：adapter 只负责初始化和极其有限的快捷操作，核心能力直接委托给标准 SDK，业务代码持有原始客户端实例。
2. **可选值用指针**：nil 表示未设置，使用 SDK 默认值。
3. **每个服务独立可选**：Config 中某个服务为 nil 则跳过初始化。
4. **不过度抽象**：不使用接口注册表，直接结构体 + 方法。
5. **配置兼容**：所有 Config 结构体支持 mapstructure / json / yaml 反序列化，可直接从 configx (viper) 加载，不强制依赖 configx。
6. **初始化失败互不影响**：单个服务初始化或 Ping 失败只记日志，不阻止其他服务初始化。

## 统一入口

### Config

```go
type Config struct {
    Redis      *RedisConfig
    MySQL      *MySQLConfig
    ES         *ESConfig
    Kafka      *KafkaConfig
    Prometheus *PrometheusConfig
    Mongo      *MongoConfig
}
```

只有非 nil 的服务才会被初始化。

### Client

```go
type Client struct {
    redis      *Redis
    mysql      *MySQL
    es         *ES
    kafka      *Kafka
    prometheus *Prometheus
    mongo      *Mongo
}
```

各字段在对应服务未配置时为 nil。

### 初始化

```go
// 手动构造配置
client, err := adapter.New(adapter.Config{
    Redis: &adapter.RedisConfig{Addr: "localhost:6379"},
    MySQL: &adapter.MySQLConfig{DSN: "user:pass@tcp(localhost:3306)/mydb"},
})

// 从配置文件动态加载（配合 configx）
cfg, _ := configx.ReadConfig("config.yaml")
adapterCfg, err := adapter.UnmarshalConfig(cfg.All())
client, err := adapter.New(*adapterCfg)
```

`New()` 返回 `*Client` 和 `error`。error 只包含初始化过程中的致命错误。单个服务初始化失败不会影响其他服务的初始化——失败服务的 Client 字段为 nil。

### 可选参数

```go
client, err := adapter.New(cfg, adapter.WithLogger(customLogger))
```

## 从配置文件反序列化

```go
func UnmarshalConfig(data map[string]any) (*Config, error)
```

这是一个便捷辅助，默认约定配置键名与结构体字段一致（小写）。

默认约定映射：

| 配置键 | 结构体字段 |
|--------|-----------|
| `redis.addr` | `RedisConfig.Addr` |
| `redis.password` | `RedisConfig.Password` |
| `redis.db` | `RedisConfig.DB` |
| `mysql.dsn` | `MySQLConfig.DSN` |
| `es.addresses` | `ESConfig.Addresses` |
| `kafka.brokers` | `KafkaConfig.Brokers` |
| `mongo.uri` | `MongoConfig.URI` |

如果配置文件 key 路径不同（例如 `cache.redis.addr`），请手动从 map 中取值构造 Config，不要依赖此方法。

配置文件示例：

```yaml
redis:
  addr: "localhost:6379"
  db: 0
mysql:
  dsn: "user:pass@tcp(localhost:3306)/mydb"
kafka:
  brokers: ["localhost:9092"]
```

---

## Redis（`redis.go`）

### RedisConfig

| 字段 | 类型 | 说明 |
|------|------|------|
| `Addr` | `string` | 服务器地址，必填，如 `"localhost:6379"` |
| `Password` | `string` | 密码，无密码时留空 |
| `DB` | `int` | 数据库编号，默认 0 |
| `MaxRetry` | `int` | 最大重试次数，0 表示 SDK 默认值 3 |
| `DialTimeout` | `time.Duration` | 连接超时，0 表示默认 5s |
| `ReadTimeout` | `time.Duration` | 读超时，0 表示默认 3s |
| `WriteTimeout` | `time.Duration` | 写超时，0 表示默认 3s |
| `PoolSize` | `int` | 连接池大小，0 表示 SDK 默认值 |

### 方法

| 方法 | 说明 |
|------|------|
| `Client() *redis.Client` | 返回底层客户端，使用 go-redis/v9 全部能力 |
| `Ping(ctx) error` | 检查连接是否可用 |
| `Close() error` | 关闭连接池 |
| `Get(ctx, key) (string, error)` | 获取 key 的值，不存在返回 redis.Nil |
| `Set(ctx, key, val, expiration) error` | 设置 key-value，expiration 为 0 表示不过期 |
| `SetEX(ctx, key, val, seconds) error` | 设置 key-value 并指定过期时间（秒） |
| `Del(ctx, keys...) error` | 删除一个或多个 key |
| `Exists(ctx, keys...) (int64, error)` | 判断 key 是否存在，返回存在数量 |
| `MGet(ctx, keys...) ([]any, error)` | 批量获取，返回切片长度与 keys 一致 |
| `Incr(ctx, key) (int64, error)` | 整数值加 1 |
| `Decr(ctx, key) (int64, error)` | 整数值减 1 |
| `Expire(ctx, key, seconds) error` | 设置过期时间（秒） |
| `TTL(ctx, key) (time.Duration, error)` | 获取剩余过期时间 |

### 使用示例

```go
rdb := client.Redis().Client()
err := rdb.Set(ctx, "key", "value", 0).Err()

// 或使用快捷方法
err = client.Redis().Set(ctx, "key", "value", 0)
```

---

## MySQL（`mysql.go`）

### MySQLConfig

| 字段 | 类型 | 说明 |
|------|------|------|
| `DSN` | `string` | 数据源名称，必填，格式：`username:password@tcp(host:port)/database?param=value` |
| `MaxOpenConns` | `int` | 最大打开连接数，0 表示 SDK 默认 |
| `MaxIdleConns` | `int` | 最大空闲连接数，0 表示 SDK 默认 |
| `ConnMaxLifetime` | `time.Duration` | 连接最大生命周期，0 表示 SDK 默认 |

### 方法

| 方法 | 说明 |
|------|------|
| `DB() *sqlx.DB` | 返回底层 sqlx.DB，使用 sqlx 全部能力 |
| `SQL() *sql.DB` | 返回底层 sql.DB，用于标准库 database/sql 操作 |
| `Ping() error` | 检查连接是否可用 |
| `Close() error` | 关闭连接池 |
| `Exec(ctx, query, args...) (sql.Result, error)` | 执行非查询 SQL（INSERT/UPDATE/DELETE） |
| `NamedExec(ctx, query, args) (sql.Result, error)` | 使用命名参数执行 SQL，args 支持 map[string]any 或 struct |
| `Query(ctx, dest, query, args...) error` | 执行查询，dest 必须为指针切片，如 `&[]User{}` |
| `Get(ctx, dest, query, args...) error` | 查询单行，dest 必须为指针，如 `&user` |

### 使用示例

```go
db := client.MySQL().DB()
users := []User{}
err := db.Select(&users, "SELECT * FROM users")

// 或使用快捷方法
err = client.MySQL().Query(ctx, &users, "SELECT * FROM users WHERE age > ?", 18)
```

---

## Elasticsearch（`es.go`）

### ESConfig

| 字段 | 类型 | 说明 |
|------|------|------|
| `Addresses` | `[]string` | ES 节点地址列表，必填，如 `[]string{"http://localhost:9200"}` |
| `Username` | `string` | 基本认证用户名 |
| `Password` | `string` | 基本认证密码 |

### 方法

| 方法 | 说明 |
|------|------|
| `Client() *elasticsearch.Client` | 返回底层 ES 客户端 |
| `Ping() error` | 检查连接是否可用（调用 Info API） |
| `Transport() interface{}` | 返回底层 Transport，用于自定义配置 |
| `Close() error` | 无需显式关闭（基于 HTTP 连接池自动管理） |
| `Index(ctx, index, id, doc) error` | 索引文档，doc 会被 JSON 序列化，id 为空时 ES 自动生成 |
| `Get(ctx, index, id, dest) error` | 获取文档，结果反序列化到 dest，不存在时返回 nil |
| `Delete(ctx, index, id) error` | 删除文档 |
| `Search(ctx, index, query, dest) error` | 执行搜索，query 为 ES Query DSL，结果反序列化到 dest |

### 使用示例

```go
esClient := client.ES().Client()

// 或使用快捷方法
err = client.ES().Index(ctx, "users", "1", map[string]any{"name": "lion"})
var result map[string]any
err = client.ES().Get(ctx, "users", "1", &result)
```

---

## Kafka（`kafka.go`）

### KafkaConfig

| 字段 | 类型 | 说明 |
|------|------|------|
| `Brokers` | `[]string` | Kafka 服务器地址列表，必填，如 `[]string{"localhost:9092"}` |
| `ClientID` | `string` | 客户端标识 |

### 方法

| 方法 | 说明 |
|------|------|
| `Client() *kgo.Client` | 返回底层 franz-go 客户端 |
| `Ping(ctx) error` | 检查连接是否可用 |
| `Produce(ctx, topic, key, value) error` | 同步发送单条消息 |
| `Consume(ctx, topics, f) error` | 消费指定 topic，f 回调接收每条消息，返回 false 时停止消费（阻塞调用） |
| `Close() error` | 关闭客户端 |

### 使用示例

```go
// 发送消息
err = client.Kafka().Produce(ctx, "my-topic", []byte("key"), []byte("hello"))

// 消费消息
err = client.Kafka().Consume(ctx, []string{"my-topic"}, func(record *kgo.Record) bool {
    fmt.Printf("key=%s value=%s\n", record.Key, record.Value)
    return true // 返回 false 停止消费
})
```

---

## MongoDB（`mongo.go`）

### MongoConfig

| 字段 | 类型 | 说明 |
|------|------|------|
| `URI` | `string` | 连接字符串，必填，如 `"mongodb://localhost:27017"` |

### 方法

| 方法 | 说明 |
|------|------|
| `Client() *mongo.Client` | 返回底层 mongo 客户端 |
| `Database(name) *mongo.Database` | 返回指定数据库的操作入口 |
| `Collection(db, coll) *mongo.Collection` | 返回指定集合的操作入口 |
| `Close() error` | 关闭连接（10s 超时） |

### 使用示例

```go
mongoClient := client.Mongo().Client()
collection := client.Mongo().Collection("my-db", "users")
// 或使用底层客户端
db := mongoClient.Database("my-db")
```

---

## Prometheus（`prometheus.go`）

### PrometheusConfig

| 字段 | 类型 | 说明 |
|------|------|------|
| `Namespace` | `string` | 指标命名空间 |
| `Subsystem` | `string` | 指标子系统 |
| `ListenAddr` | `string` | HTTP 监听地址，留空则不启动抓取端点，如 `":9090"` |

### 方法

| 方法 | 说明 |
|------|------|
| `Registry() *prometheus.Registry` | 返回底层 Registry |
| `Register(c) error` | 注册指标采集器 |
| `MustRegister(c)` | 注册指标采集器，失败时 panic |
| `Unregister(c) bool` | 注销指标采集器 |
| `NewCounter(opt) prometheus.Counter` | 创建并注册 Counter |
| `NewGauge(opt) prometheus.Gauge` | 创建并注册 Gauge |
| `NewHistogram(opt) prometheus.Histogram` | 创建并注册 Histogram |
| `NewSummary(opt) prometheus.Summary` | 创建并注册 Summary |
| `Gather() ([]*dto.MetricFamily, error)` | 收集所有注册的指标样本 |
| `Close() error` | 无需显式关闭，保留以统一接口 |

### 使用示例

```go
counter := client.Prometheus().NewCounter(prometheus.CounterOpts{
    Name: "requests_total",
    Help: "Total number of requests",
})
counter.Inc()

registry := client.Prometheus().Registry()
http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
```

---

## 错误处理

### AdapterError

```go
type AdapterError struct { ... }

func (e *AdapterError) Error() string
func (e *AdapterError) Code() string
func IsAdapterError(err error) bool
```

### 预定义错误码

| 错误 | 码 |
|------|----|
| `ErrConfigRequired` | `config_required` |
| `ErrRedisInitFailed` | `redis_init_failed` |
| `ErrMySQLInitFailed` | `mysql_init_failed` |
| `ErrESInitFailed` | `es_init_failed` |
| `ErrKafkaInitFailed` | `kafka_init_failed` |
| `ErrMongoInitFailed` | `mongo_init_failed` |
| `ErrPrometheusFailed` | `prometheus_init_failed` |

---

## 依赖

- [github.com/Effortful-lion/unibase/logx](https://github.com/Effortful-lion/unibase/tree/main/logx) — 日志模块
