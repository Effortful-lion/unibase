package configx

import (
	"sync"

	"github.com/spf13/viper"
)

//========================================导出方法=======================================

// Config 是配置的核心结构，封装了 viper 实例。
// 线程安全：所有读写方法内部加锁保护。
type Config struct {
	v        *viper.Viper
	mu       sync.RWMutex
	onChange []func()
}

// New 从指定配置文件路径创建 Config。
// 支持格式：yaml, yml, json, toml, properties, hcl, env
func New(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	return &Config{v: v}, nil
}

// Get 读取配置值，返回原始接口。
// key 使用点号分隔：如 "server.port"。
func (c *Config) Get(key string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v.Get(key)
}

// GetString 读取字符串配置。
func (c *Config) GetString(key string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v.GetString(key)
}

// GetInt 读取整数配置。
func (c *Config) GetInt(key string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v.GetInt(key)
}

// GetBool 读取布尔配置。
func (c *Config) GetBool(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v.GetBool(key)
}

// GetFloat64 读取浮点数配置。
func (c *Config) GetFloat64(key string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v.GetFloat64(key)
}

// GetStringSlice 读取字符串切片配置。
func (c *Config) GetStringSlice(key string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v.GetStringSlice(key)
}

// Set 设置配置值。
// 注意：Set 仅在内存中修改，不会写入文件。
func (c *Config) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.v.Set(key, value)
}

// All 返回所有配置的映射（嵌套 map）。
func (c *Config) All() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v.AllSettings()
}

// OnChange 注册配置变更回调。
// 多个回调按注册顺序执行。
func (c *Config) OnChange(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onChange = append(c.onChange, fn)
}

// Unmarshal 将整个配置解码到目标结构体中。
// target 必须为指针。结构体字段通过 mapstructure tag 或 json tag 绑定。
//
//	cfg, _ := configx.ReadConfig("config.yaml")
//	var appCfg AppConfig
//	if err := cfg.Unmarshal(&appCfg); err != nil { ... }
func (c *Config) Unmarshal(target any) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v.Unmarshal(target)
}

// UnmarshalKey 将指定子配置解码到目标结构体中。
// 例如：将 "database" 段解码到 DatabaseConfig。
//
//	cfg, _ := configx.ReadConfig("config.yaml")
//	var dbCfg DatabaseConfig
//	if err := cfg.UnmarshalKey("database", &dbCfg); err != nil { ... }
func (c *Config) UnmarshalKey(key string, target any) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v.UnmarshalKey(key, target)
}

//========================================非导出方法=======================================

// runOnChange 触发所有注册的回调（内部方法）。
func (c *Config) runOnChange() {
	c.mu.RLock()
	fns := make([]func(), len(c.onChange))
	copy(fns, c.onChange)
	c.mu.RUnlock()

	for _, fn := range fns {
		fn()
	}
}
