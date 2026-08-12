/*
Package adapter 提供第三方服务客户端的薄初始化封装。

当前支持的服务：

	Redis       — github.com/redis/go-redis/v9
	MySQL       — github.com/jmoiron/sqlx（基于 database/sql）
	Elasticsearch — github.com/elastic/go-elasticsearch/v8
	Kafka       — github.com/twmb/franz-go
	MongoDB     — go.mongodb.org/mongo-driver/v2
	Prometheus  — github.com/prometheus/client_golang

设计原则：

 1. 薄封装：adapter 只负责初始化和极其有限的快捷操作，
    核心能力直接委托给标准 SDK，业务代码持有原始客户端实例。
 2. 可选值用指针：nil 表示未设置，使用 SDK 默认值。
 3. 每个服务独立可选：Config 中某个服务为 nil 则跳过初始化。
 4. 不过度抽象：不使用接口注册表，直接结构体 + 方法。
 5. 配置兼容：所有 Config 结构体支持 mapstructure / json / yaml 反序列化，
    可直接从 configx (viper) 加载，不强制依赖 configx。

典型用法：

	// 方式一：手动构造配置
	client, err := adapter.New(adapter.Config{
		Redis: &adapter.RedisConfig{Addr: "localhost:6379"},
		MySQL: &adapter.MySQLConfig{DSN: "user:pass@tcp(localhost:3306)/mydb"},
	})

	// 方式二：从配置文件动态加载（配合 configx）
	cfg, _ := configx.ReadConfig("config.yaml")
	adapterCfg, err := adapter.UnmarshalConfig(cfg.All())
	if err != nil {
		log.Fatal(err)
	}
	client, err := adapter.New(*adapterCfg)

	// 配置文件示例 (config.yaml):
	//   redis:
	//     addr: "localhost:6379"
	//     db: 0
	//   mysql:
	//     dsn: "user:pass@tcp(localhost:3306)/mydb"
	//   kafka:
	//     brokers: ["localhost:9092"]

	// 使用原始 SDK 实例
	rdb := client.Redis().Client()
	err = rdb.Set(ctx, "key", "value", 0).Err()

	db := client.MySQL().DB()
	users := []User{}
	err := db.Select(&users, "SELECT * FROM users")
*/
package adapter
