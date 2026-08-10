// Package configx 提供了基于 viper 封装的轻量配置管理能力。
//
// 核心能力：
//   - 从文件加载配置（支持 YAML / JSON / TOML），支持向上搜索文件名
//   - 类型化读取配置值（GetString / GetInt / GetBool / GetFloat64 / GetStringSlice）
//   - 将配置绑定到业务 struct（Unmarshal / UnmarshalKey）
//   - 监听配置文件变化，热加载 + 回调通知
//   - 加载 .env 文件到环境变量
//
// 快速开始：
//
//	// 定义业务配置结构
//	type AppConfig struct {
//	    Server ServerConfig `mapstructure:"server"`
//	}
//
//	// 加载并绑定
//	cfg, _ := configx.ReadConfig("config.yaml")
//	var appCfg AppConfig
//	cfg.Unmarshal(&appCfg)
//
//	// 读取
//	port := appCfg.Server.Port
package configx
