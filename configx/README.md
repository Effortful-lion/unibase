# 定位

这是一个基于 viper 封装的一个常用配置库（部分可能引入 goenvdot）

# 演进方向

前期只封装部分常用的，后期缓慢扩展。需要用到其他的可以临时直接用 viper 库

# configx

基于 viper 封装的轻量配置库。内部屏蔽 viper 细节，对外提供类型化 Getter + struct 绑定。

## 快速开始

```go
// 1. 定义业务配置结构
type AppConfig struct {
    Server   ServerConfig   `mapstructure:"server"`
    Database DatabaseConfig `mapstructure:"database"`
}

// 2. 加载并绑定
cfg, err := configx.ReadConfig("config.yaml")
if err != nil {
    log.Fatal(err)
}
var appCfg AppConfig
if err := cfg.Unmarshal(&appCfg); err != nil {
    log.Fatal(err)
}

// 3. 直接使用强类型字段
addr := appCfg.Server.Host + ":" + strconv.Itoa(appCfg.Server.Port)
```

## 完整示例

```go
package main

import (
    "fmt"
    "log"

    "github.com/Effortful-lion/unibase/configx"
)

type AppConfig struct {
    Server   ServerConfig   `mapstructure:"server"`
    Database DatabaseConfig `mapstructure:"database"`
    App      AppInfo        `mapstructure:"app"`
}

type ServerConfig struct {
    Host    string `mapstructure:"host"`
    Port    int    `mapstructure:"port"`
    Timeout int    `mapstructure:"timeout"` // 可选，YAML 无此字段则为 0
}

type DatabaseConfig struct {
    DSN     string `mapstructure:"dsn"`
    MaxOpen int    `mapstructure:"max_open"`
    MaxIdle int    `mapstructure:"max_idle"`
}

type AppInfo struct {
    Name string `mapstructure:"name"`
}

func main() {
    // 加载（支持绝对路径 / 相对路径 / 仅文件名向上搜索）
    cfg, err := configx.ReadConfig("config.yaml")
    if err != nil {
        log.Fatal(err)
    }

    // 绑定到 struct
    var appCfg AppConfig
    if err := cfg.Unmarshal(&appCfg); err != nil {
        log.Fatal(err)
    }

    // 也可以用 UnmarshalKey 只绑定某个子段
    var db DatabaseConfig
    cfg.UnmarshalKey("database", &db)

    fmt.Printf("server: %s:%d\n", appCfg.Server.Host, appCfg.Server.Port)
    fmt.Printf("app: %s\n", appCfg.App.Name)

    // 注册热加载回调
    cfg.OnChange(func() {
        // 重新 Unmarshal 刷新结构体
    })

    // 启动监听（独立 goroutine）
    go cfg.Watch(configx.WatchOptions{})
}
```

## config.yaml 示例

```yaml
server:
  host: 0.0.0.0
  port: 8080
  timeout: 30

database:
  dsn: user:pass@tcp(db:3306)/mydb
  max_open: 100
  max_idle: 10

app:
  name: my-service
```

## API

### 加载配置

| 方法 | 说明 |
|------|------|
| `New(path string) (*Config, error)` | 从绝对/相对路径加载 |
| `ReadConfig(path string) (*Config, error)` | 路径不存在时，从当前目录向上搜索同名文件 |

### 读取配置

| 方法 | 说明 |
|------|------|
| `Get(key string) any` | 返回原始值，key 用点号分隔，如 `"server.port"` |
| `GetString(key string) string` | 读字符串 |
| `GetInt(key string) int` | 读整数 |
| `GetBool(key string) bool` | 读布尔 |
| `GetFloat64(key string) float64` | 读浮点 |
| `GetStringSlice(key string) []string` | 读字符串切片 |
| `All() map[string]any` | 返回全部配置（嵌套 map） |

### 绑定到 struct

| 方法 | 说明 |
|------|------|
| `Unmarshal(target any) error` | 将整个配置解码到 struct，target 必须为指针 |
| `UnmarshalKey(key string, target any) error` | 将指定子段（如 `"database"`）解码到 struct |

字段通过 `mapstructure` tag 绑定（也可同时声明 `json` tag 兼容 JSON 序列化）。

### 写入配置

| 方法 | 说明 |
|------|------|
| `Set(key string, value any)` | 内存中修改，不写入文件 |

### 监听变更

| 方法 | 说明 |
|------|------|
| `OnChange(fn func())` | 注册配置变更回调，多个按注册顺序执行 |
| `Watch(opts WatchOptions) error` | 启动文件监听，热加载 + 触发回调，阻塞运行 |

```go
cfg.OnChange(func() {
    // 重新 Unmarshal 刷新结构体
})
go cfg.Watch(configx.WatchOptions{})
```

`WatchOptions` 默认值：`Interval 100ms`，`Debounce 200ms`。

### 环境变量

| 方法 | 说明 |
|------|------|
| `LoadEnv(paths ...string) error` | 加载 `.env` 文件到环境变量，后加载覆盖先加载 |

```go
// 默认加载当前目录 .env
configx.LoadEnv()

// 或指定多个，后面的覆盖前面的
configx.LoadEnv(".env", ".env.local")
```

## 结构体 Tag 说明

```go
// 只绑定配置 → mapstructure 足够
type Server struct {
    Port int `mapstructure:"port"`
}

// 同时需要 JSON 序列化 → 同时声明两种 tag
type Server struct {
    Port int `mapstructure:"port" json:"port"`
}
```

## 缺失字段行为

`Unmarshal` 不会覆盖 struct 中已有的非零值。YAML 中不存在的字段保持 struct 零值，不会报错。因此可以在 `Unmarshal` 前设置默认值：

```go
var cfg AppConfig
cfg.Server.Host = "127.0.0.1" // 默认值
cfg.Unmarshal(&appCfg)
// YAML 有值则覆盖，无值则保留 "127.0.0.1"
```

## 文件结构

```
configx/
├── doc.go              # 包文档
├── config.go           # Config 结构体 + Getter/Setter/Unmarshal
├── read_config.go      # ReadConfig（向上搜索文件名）
├── watch_config.go     # Watch + OnChange
├── env.go              # LoadEnv
├── README.md           # 本文档
└── config_test.go      # 测试
```

## 依赖

- `github.com/spf13/viper` — 配置读取与 mapstructure 解码
- `github.com/fsnotify/fsnotify` — 文件监听
- `github.com/joho/godotenv` — .env 文件加载
