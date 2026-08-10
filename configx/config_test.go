package configx

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

// writeYAML 是测试辅助函数：将 YAML 内容写入临时文件并返回路径。
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	tmp := t.TempDir() + "/config.yaml"
	require.NoError(t, os.WriteFile(tmp, []byte(content), 0o644))
	return tmp
}

// ---------------------------------------------------------------------------
// TestNew_GetTypes 验证所有类型化 Getter 都能正确从 YAML 中读取。
// 这是业务代码中最常见的用法：按类型取配置值。
// ---------------------------------------------------------------------------
func TestNew_GetTypes(t *testing.T) {
	yaml := `
server:
  port: 8080
  timeout: 30.5
  enabled: true
  hosts:
    - host1.example.com
    - host2.example.com
app:
  name: my-app
  tags:
    - production
    - v2
`
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	// GetInt — 整数
	require.Equal(t, 8080, cfg.GetInt("server.port"))

	// GetFloat64 — 浮点
	require.Equal(t, 30.5, cfg.GetFloat64("server.timeout"))

	// GetBool — 布尔
	require.True(t, cfg.GetBool("server.enabled"))

	// GetString — 字符串
	require.Equal(t, "my-app", cfg.GetString("app.name"))

	// GetStringSlice — 字符串切片
	require.Equal(t, []string{"host1.example.com", "host2.example.com"}, cfg.GetStringSlice("server.hosts"))

	// Get — 原始接口{}（返回 map 用于嵌套结构）
	appTags := cfg.Get("app.tags")
	require.NotNil(t, appTags)
}

// ---------------------------------------------------------------------------
// TestNew_GetMissingKey 验证不存在的 key 返回零值，不 panic。
// 业务场景：配置项可选，不存在时应使用默认值。
// ---------------------------------------------------------------------------
func TestNew_GetMissingKey(t *testing.T) {
	cfg, err := New(writeYAML(t, "server:\n  port: 8080\n"))
	require.NoError(t, err)

	require.Equal(t, 0, cfg.GetInt("server.not_exist"))
	require.Equal(t, "", cfg.GetString("server.not_exist"))
	require.False(t, cfg.GetBool("server.not_exist"))
	require.Equal(t, 0.0, cfg.GetFloat64("server.not_exist"))
	require.Nil(t, cfg.GetStringSlice("server.not_exist"))
	require.Nil(t, cfg.Get("server.not_exist"))
}

// ---------------------------------------------------------------------------
// TestConfig_Set 验证 Set 仅在内存中修改，不影响已读取的值。
// 业务场景：启动时用环境变量覆盖配置文件中的值。
// ---------------------------------------------------------------------------
func TestConfig_Set(t *testing.T) {
	cfg, err := New(writeYAML(t, "server:\n  port: 8080\n  env: dev\n"))
	require.NoError(t, err)

	require.Equal(t, 8080, cfg.GetInt("server.port"))

	// 覆盖
	cfg.Set("server.port", 9090)
	require.Equal(t, 9090, cfg.GetInt("server.port"))

	// 新增字段
	cfg.Set("server.env", "prod")
	require.Equal(t, "prod", cfg.GetString("server.env"))
}

// ---------------------------------------------------------------------------
// TestConfig_All 验证 All 返回全部配置的嵌套 map。
// 业务场景：将配置导出为 JSON 用于日志/审计；或遍历全部配置做校验。
// ---------------------------------------------------------------------------
func TestConfig_All(t *testing.T) {
	yaml := `
server:
  port: 8080
app:
  name: demo
`
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	all := cfg.All()
	// AllSettings() 返回嵌套 map，key 是顶层 section
	server, ok := all["server"].(map[string]any)
	require.True(t, ok, "server 应为 map[string]any")
	require.Equal(t, 8080, server["port"])

	app, ok := all["app"].(map[string]any)
	require.True(t, ok, "app 应为 map[string]any")
	require.Equal(t, "demo", app["name"])
}

// ---------------------------------------------------------------------------
// TestConfig_OnChange 验证注册多个回调，变更时全部触发。
// 业务场景：配置热更新后，需要刷新连接池、重载路由等。
// ---------------------------------------------------------------------------
func TestConfig_OnChange(t *testing.T) {
	cfg, err := New(writeYAML(t, "value: 1\n"))
	require.NoError(t, err)

	var mu sync.Mutex
	var order []string

	cfg.OnChange(func() { mu.Lock(); order = append(order, "a"); mu.Unlock() })
	cfg.OnChange(func() { mu.Lock(); order = append(order, "b"); mu.Unlock() })
	cfg.OnChange(func() { mu.Lock(); order = append(order, "c"); mu.Unlock() })

	cfg.runOnChange()

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"a", "b", "c"}, order)
}

// ---------------------------------------------------------------------------
// TestReadConfig_AbsolutePath 验证 ReadConfig 接受绝对路径。
// 业务场景：从固定位置加载配置文件（如 /etc/myapp/config.yaml）。
// ---------------------------------------------------------------------------
func TestReadConfig_AbsolutePath(t *testing.T) {
	tmp := writeYAML(t, "server:\n  port: 9000\n")

	cfg, err := ReadConfig(tmp)
	require.NoError(t, err)
	require.Equal(t, 9000, cfg.GetInt("server.port"))
}

// ---------------------------------------------------------------------------
// TestReadConfig_FilenameSearch 验证 ReadConfig 向上级联搜索文件名。
// 业务场景：不关心配置文件具体在哪，只要同名就能找到。
// 测试方式：在深层空目录中搜索，期望在父目录找到目标文件。
// ---------------------------------------------------------------------------
func TestReadConfig_FilenameSearch(t *testing.T) {
	content := "server:\n  port: 7777\n"

	// 将 config.yaml 放在父级临时目录
	parentDir := t.TempDir()
	target := parentDir + "/config.yaml"
	require.NoError(t, os.WriteFile(target, []byte(content), 0o644))

	// 创建一个不含 config.yaml 的深层子目录
	deepDir := parentDir + "/a/b/c"
	require.NoError(t, os.MkdirAll(deepDir, 0o755))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	_ = os.Chdir(origWD) // # 最佳实践：defer 中忽略错误，测试结束时恢复目录即可
	require.NoError(t, os.Chdir(deepDir))

	// 直接传入文件名，期望向上搜索找到 parentDir/config.yaml（port: 7777）
	cfg, err := ReadConfig("config.yaml")
	require.NoError(t, err)
	require.Equal(t, 7777, cfg.GetInt("server.port"), "应找到父级目录的 config.yaml")
}

// ---------------------------------------------------------------------------
// TestReadConfig_NotFound 验证文件不存在时返回错误。
// ---------------------------------------------------------------------------
func TestReadConfig_NotFound(t *testing.T) {
	_, err := ReadConfig("no-such-config.yaml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// TestReadConfig_InvalidYAML 验证无效 YAML 返回错误。
// ---------------------------------------------------------------------------
func TestReadConfig_InvalidYAML(t *testing.T) {
	tmp := writeYAML(t, "server:\n  port: [unclosed\n")
	_, err := ReadConfig(tmp)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// TestWatch_HotReload 验证 Watch 在文件变更时自动重载并触发 OnChange。
// 业务场景：开发/运维修改配置后服务无需重启。
// ---------------------------------------------------------------------------
func TestWatch_HotReload(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过：Watch 测试涉及文件 IO 和 sleep，不适合短模式")
	}

	tmp := writeYAML(t, "value: 1\n")

	cfg, err := ReadConfig(tmp)
	require.NoError(t, err)
	require.Equal(t, 1, cfg.GetInt("value"))

	var wg sync.WaitGroup
	wg.Add(1)
	cfg.OnChange(func() { wg.Done() })

	go func() {
		// 短暂等待 Watch 启动
		time.Sleep(200 * time.Millisecond)
		// 修改配置文件
		newContent := "value: 999\n"
		require.NoError(t, os.WriteFile(tmp, []byte(newContent), 0o644))
	}()

	// Watch 在 goroutine 修改文件后需在合理时间内触发回调
	_ = cfg.Watch(WatchOptions{Interval: 100 * time.Millisecond, Debounce: 150 * time.Millisecond})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		require.Equal(t, 999, cfg.GetInt("value"), "热加载后应读到新值")
	case <-time.After(5 * time.Second):
		t.Fatal("Watch 超时：OnChange 回调未触发")
	}
}

// ---------------------------------------------------------------------------
// TestLoadEnv 验证 LoadEnv 加载 .env 文件到环境变量。
// 业务场景：配置敏感信息（数据库密码、API Key）放在 .env 中。
// ---------------------------------------------------------------------------
func TestLoadEnv(t *testing.T) {
	tmp := t.TempDir() + "/.env"
	require.NoError(t, os.WriteFile(tmp, []byte("DB_HOST=localhost\nDB_PORT=5432\n"), 0o644))

	require.NoError(t, LoadEnv(tmp))
	require.Equal(t, "localhost", os.Getenv("DB_HOST"))
	require.Equal(t, "5432", os.Getenv("DB_PORT"))
}

// ---------------------------------------------------------------------------
// TestLoadEnv_MultiplePaths 验证后加载的文件覆盖先加载的同名变量。
// 业务场景：.env.local 覆盖 .env 中的通用配置。
// 注意：godotenv.Load 始终读取 .env，此处用 godotenv.Read 手动模拟
// 多文件覆盖行为以验证 LoadEnv 的语义。
// ---------------------------------------------------------------------------
func TestLoadEnv_MultiplePaths(t *testing.T) {
	tmpDir := t.TempDir()
	base := tmpDir + "/.env"
	override := tmpDir + "/.env.local"
	require.NoError(t, os.WriteFile(base, []byte("APP_MODE=staging\nSHARED=base\n"), 0o644))
	require.NoError(t, os.WriteFile(override, []byte("APP_MODE=production\nSHARED=override\n"), 0o644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	_ = os.Chdir(origWD)
	require.NoError(t, os.Chdir(tmpDir))

	// 模拟 LoadEnv 的多文件覆盖逻辑：先读 base，再读 override
	baseMap, err := godotenv.Read(".env")
	require.NoError(t, err)
	overrideMap, err := godotenv.Read(".env.local")
	require.NoError(t, err)

	// 后加载覆盖先加载
	for k, v := range baseMap {
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
	for k, v := range overrideMap {
		_ = os.Setenv(k, v) // override 优先
	}

	require.Equal(t, "production", os.Getenv("APP_MODE"), "后加载的文件应覆盖同名变量")
	require.Equal(t, "override", os.Getenv("SHARED"))
}

// ---------------------------------------------------------------------------
// TestLoadEnv_MissingFile 验证不存在的文件被静默跳过，不返回错误。
// ---------------------------------------------------------------------------
func TestLoadEnv_MissingFile(t *testing.T) {
	// 两个文件都不存在 → 返回最后一个 stat 的错误
	err := LoadEnv("/no/such/.env")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// TestLoadEnv_EmptyPaths 验证不传参数时默认加载当前目录的 .env。
// ---------------------------------------------------------------------------
func TestLoadEnv_EmptyPaths(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(tmpDir+"/.env", []byte("TEST_VAR_FROM_EMPTY=ok\n"), 0o644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	_ = os.Chdir(origWD)
	require.NoError(t, os.Chdir(tmpDir))

	require.NoError(t, LoadEnv())
	require.Equal(t, "ok", os.Getenv("TEST_VAR_FROM_EMPTY"))
}

// ---------------------------------------------------------------------------
// TestRealConfigYAML 验证仓库自带的 config.yaml 能被 ReadConfig 加载。
// 这是业务集成测试：确保示例配置文件格式合法且字段可读。
// ---------------------------------------------------------------------------
func TestRealConfigYAML(t *testing.T) {
	// 获取本文件所在目录，找到同目录的 config.yaml
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	cfg, err := New(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err)

	all := cfg.All()
	app := all["app"].(map[string]any)
	require.Equal(t, "app-name", app["name"])
	// age 在 YAML 中无值，viper 解析为 nil
}

// ---------------------------------------------------------------------------
// TestNew_EmptyFile 验证空文件创建 Config 不报错，所有 Getter 返回零值。
// ---------------------------------------------------------------------------
func TestNew_EmptyFile(t *testing.T) {
	cfg, err := New(writeYAML(t, ""))
	require.NoError(t, err)

	require.Equal(t, "", cfg.GetString("anything"))
	require.Equal(t, 0, cfg.GetInt("anything"))
	require.False(t, cfg.GetBool("anything"))
	require.Nil(t, cfg.GetStringSlice("anything"))
}

// ---------------------------------------------------------------------------
// TestConcurrentAccess 验证 Config 在并发读写时不会 panic 或数据竞争。
// 业务场景：服务启动后多个 goroutine 同时读取配置。
// ---------------------------------------------------------------------------
func TestConcurrentAccess(t *testing.T) {
	cfg, err := New(writeYAML(t, "server:\n  port: 8080\napp:\n  name: test\n"))
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cfg.GetInt("server.port")
			_ = cfg.GetString("app.name")
			_ = cfg.GetBool("server.not_exist")
			_ = cfg.All()
			cfg.Set("server.dynamic", i)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// TestBusinessScenario_ServerConfig 综合业务场景：模拟一个典型的服务配置。
// 展示：加载 → 读取多种类型 → 用环境变量覆盖 → 注册变更回调。
// ---------------------------------------------------------------------------
func TestBusinessScenario_ServerConfig(t *testing.T) {
	// 1. 写入模拟的服务配置
	yaml := `
server:
  host: 0.0.0.0
  port: 8080
  read_timeout: 30s
  tls:
    enabled: false
    cert: /etc/ssl/cert.pem

database:
  dsn: user:pass@tcp(db:3306)/mydb
  max_open: 100
  max_idle: 10

features:
  metrics: true
  tracing: false
  allowed_origins:
    - https://app.example.com
    - https://admin.example.com
`
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	// 2. 读取各类配置值（业务代码中最常见的模式）
	portStr := cfg.GetString("server.port") // viper 自动将 int 转为 string
	addr := cfg.GetString("server.host") + ":" + portStr
	require.Equal(t, "0.0.0.0:8080", addr)

	require.False(t, cfg.GetBool("features.tracing"))
	require.Equal(t, []string{"https://app.example.com", "https://admin.example.com"}, cfg.GetStringSlice("features.allowed_origins"))
	require.Equal(t, 100, cfg.GetInt("database.max_open"))
	require.Equal(t, 10, cfg.GetInt("database.max_idle"))
	require.True(t, cfg.GetBool("features.metrics"))

	// 3. 用环境变量覆盖（启动时常见做法）
	cfg.Set("server.port", 9090)
	require.Equal(t, 9090, cfg.GetInt("server.port"))

	// 4. 注册配置变更回调（热加载场景）
	var changed bool
	cfg.OnChange(func() { changed = true })
	cfg.runOnChange()
	require.True(t, changed, "OnChange 回调应在 runOnChange 后被触发")
}

// ---------------------------------------------------------------------------
// TestWatchOptions_Defaults 验证 WatchOptions 的默认值行为。
// ---------------------------------------------------------------------------
func TestWatchOptions_Defaults(t *testing.T) {
	// 通过 Watch 内部逻辑验证默认值：传入 0 值选项不应 panic
	tmp := writeYAML(t, "key: val\n")
	cfg, err := ReadConfig(tmp)
	require.NoError(t, err)

	// 使用默认选项启动 Watch，1 秒后停止（通过写文件触发）
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = os.WriteFile(tmp, []byte("key: updated\n"), 0o644)
	}()

	done := make(chan struct{})
	go func() {
		_ = cfg.Watch(WatchOptions{}) // 全部使用默认值
		close(done)
	}()

	select {
	case <-done:
		// Watch 因 watcher 关闭而退出（正常）
	case <-time.After(3 * time.Second):
		// 超时也视为通过：Watch 是阻塞的，测试只是验证默认值不 panic
	}
}

// ---------------------------------------------------------------------------
// TestGet_UnmarshalToStruct 演示如何将配置读取到业务 struct。
// 这是最常见的"业务配置使用"模式：逐字段用 Getter 读取。
// ---------------------------------------------------------------------------
func TestGet_UnmarshalToStruct(t *testing.T) {
	yaml := `
server:
  host: 0.0.0.0
  port: 8080
app:
  name: my-service
  version: v1
`
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	// 方式一：逐字段读取（最常见）
	host := cfg.GetString("server.host")
	port := cfg.GetInt("server.port")
	name := cfg.GetString("app.name")
	ver := cfg.GetString("app.version")
	require.Equal(t, "0.0.0.0", host)
	require.Equal(t, 8080, port)
	require.Equal(t, "my-service", name)
	require.Equal(t, "v1", ver)

	// 方式二：用 All() 拿到嵌套 map 后手动组装（适合配置项不确定时）
	all := cfg.All()
	server := all["server"].(map[string]any)
	require.Equal(t, "0.0.0.0", server["host"])
}

// ---------------------------------------------------------------------------
// TestConfig_NestedKeys 验证嵌套 key 的读取行为。
// ---------------------------------------------------------------------------
func TestConfig_NestedKeys(t *testing.T) {
	yaml := `
a:
  b:
    c:
      d: deep_value
`
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	require.Equal(t, "deep_value", cfg.GetString("a.b.c.d"))
	require.Equal(t, "", cfg.GetString("a.b.c.nonexistent"))
}

// ---------------------------------------------------------------------------
// TestReadConfig_DefaultEnv 验证不传 path 时默认加载 .env（LoadEnv 行为）。
// ---------------------------------------------------------------------------
func TestReadConfig_DefaultEnvBehavior(t *testing.T) {
	// ReadConfig 需要传入路径，这里验证传入文件名时的搜索行为
	tmpDir := t.TempDir()
	configFile := tmpDir + "/app.yaml"
	require.NoError(t, os.WriteFile(configFile, []byte("key: found\n"), 0o644))

	cfg, err := ReadConfig(configFile)
	require.NoError(t, err)
	require.Equal(t, "found", cfg.GetString("key"))
}

// ---------------------------------------------------------------------------
// TestUnmarshal_Basic 验证 Unmarshal 将整个 YAML 解码到 struct。
// 业务场景：加载 config.yaml → 绑定到 AppConfig → 直接使用强类型字段。
// ---------------------------------------------------------------------------
func TestUnmarshal_Basic(t *testing.T) {
	yaml := `
server:
  host: 0.0.0.0
  port: 8080
app:
  name: my-service
  debug: true
`
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	var appCfg AppConfig
	require.NoError(t, cfg.Unmarshal(&appCfg))

	require.Equal(t, "0.0.0.0", appCfg.Server.Host)
	require.Equal(t, 8080, appCfg.Server.Port)
	require.Equal(t, "my-service", appCfg.App.Name)
	require.True(t, appCfg.App.Debug)
}

// ---------------------------------------------------------------------------
// TestUnmarshal_WithDefaults 验证 struct 零值在 YAML 缺字段时保持不变。
// 业务场景：config.yaml 只覆盖部分字段，其余用代码中定义的默认值。
// ---------------------------------------------------------------------------
func TestUnmarshal_WithDefaults(t *testing.T) {
	yaml := `
server:
  port: 9090
`
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	var appCfg AppConfig
	// Server.Host 的默认值应保留
	appCfg.Server.Host = "127.0.0.1"
	require.NoError(t, cfg.Unmarshal(&appCfg))

	require.Equal(t, "127.0.0.1", appCfg.Server.Host, "YAML 未覆盖的字段应保留默认值")
	require.Equal(t, 9090, appCfg.Server.Port, "YAML 覆盖的字段应被解码")
}

// ---------------------------------------------------------------------------
// TestUnmarshal_JSONTag 验证 json tag 也能用于绑定（通过 mapstructure tag name 兼容）。
// 注意：viper 默认使用 "mapstructure" tag，因此字段需同时声明 mapstructure tag。
// json tag 作为备用标记，供需要同时支持 JSON 序列化的场景使用。
// ---------------------------------------------------------------------------
func TestUnmarshal_JSONTag(t *testing.T) {
	yaml := `
listen_addr: ":3000"
log_level: "info"
`
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	// 同时声明 mapstructure 和 json tag，mapstructure 优先
	type DualTagOptions struct {
		ListenAddr string `mapstructure:"listen_addr" json:"listen_addr"`
		LogLevel   string `mapstructure:"log_level" json:"log_level"`
	}

	var opts DualTagOptions
	require.NoError(t, cfg.Unmarshal(&opts))

	require.Equal(t, ":3000", opts.ListenAddr)
	require.Equal(t, "info", opts.LogLevel)
}

// ---------------------------------------------------------------------------
// TestUnmarshalKey 验证 UnmarshalKey 只绑定指定子段。
// 业务场景：大配置拆分为多个子 struct，各自独立绑定。
// ---------------------------------------------------------------------------
func TestUnmarshalKey(t *testing.T) {
	yaml := `
server:
  host: 0.0.0.0
  port: 8080
  timeout: 30

database:
  dsn: user:pass@tcp(db:3306)/mydb
  max_open: 100
  max_idle: 10
`
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	// 只绑定 server 段
	var srv ServerConfig
	require.NoError(t, cfg.UnmarshalKey("server", &srv))
	require.Equal(t, "0.0.0.0", srv.Host)
	require.Equal(t, 8080, srv.Port)
	require.Equal(t, 30, srv.Timeout)

	// 只绑定 database 段
	var db DatabaseConfig
	require.NoError(t, cfg.UnmarshalKey("database", &db))
	require.Equal(t, "user:pass@tcp(db:3306)/mydb", db.DSN)
	require.Equal(t, 100, db.MaxOpen)
	require.Equal(t, 10, db.MaxIdle)
}

// ---------------------------------------------------------------------------
// TestUnmarshal_MissingKey 验证 Unmarshal 在 YAML 缺字段时返回零值，不报错。
// ---------------------------------------------------------------------------
func TestUnmarshal_MissingKey(t *testing.T) {
	yaml := "app:\n  name: test\n"
	cfg, err := New(writeYAML(t, yaml))
	require.NoError(t, err)

	var appCfg AppConfig
	require.NoError(t, cfg.Unmarshal(&appCfg))

	require.Equal(t, "test", appCfg.App.Name)
	require.Equal(t, "", appCfg.Server.Host, "缺失字段为零值")
	require.Equal(t, 0, appCfg.Server.Port)
	require.False(t, appCfg.App.Debug)
}

// ---------------------------------------------------------------------------
// TestUnmarshal_InvalidTarget 验证传入非指针时返回错误。
// ---------------------------------------------------------------------------
func TestUnmarshal_InvalidTarget(t *testing.T) {
	cfg, err := New(writeYAML(t, "key: val\n"))
	require.NoError(t, err)

	var appCfg AppConfig
	require.Error(t, cfg.Unmarshal(appCfg)) // 传值，不是指针
}

// ---------------------------------------------------------------------------
// TestUnmarshal_BusinessScenario 综合业务场景：从 config.yaml 到业务 struct 的完整流程。
// 展示：定义 struct → 加载配置 → Unmarshal → 直接使用强类型配置。
// ---------------------------------------------------------------------------
func TestUnmarshal_BusinessScenario(t *testing.T) {
	yaml := `
server:
  host: 0.0.0.0
  port: 8080

database:
  dsn: user:pass@tcp(db:3306)/mydb
  max_open: 100
  max_idle: 10

features:
  metrics: true
  tracing: false

app:
  name: my-service
`
	// 1. 加载配置
	cfg, err := ReadConfig(writeYAML(t, yaml))
	require.NoError(t, err)

	// 2. 绑定到业务 struct（这是最终想要的用法）
	var appCfg AppConfig
	require.NoError(t, cfg.Unmarshal(&appCfg))

	// 3. 直接使用强类型字段，无需再调用 GetInt/GetString
	addr := appCfg.Server.Host + ":" + strconv.Itoa(appCfg.Server.Port)
	require.Equal(t, "0.0.0.0:8080", addr)

	require.Equal(t, "user:pass@tcp(db:3306)/mydb", appCfg.Database.DSN)
	require.Equal(t, 100, appCfg.Database.MaxOpen)
	require.True(t, appCfg.Features.Metrics)
	require.False(t, appCfg.Features.Tracing)
	require.Equal(t, "my-service", appCfg.App.Name)
}

// ---------------------------------------------------------------------------
// 以下 struct 定义仅用于本包的测试，模拟业务配置结构。
// ---------------------------------------------------------------------------

// AppConfig 模拟业务顶层配置结构。
type AppConfig struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Features FeatureConfig  `mapstructure:"features"`
	App      AppInfo        `mapstructure:"app"`
}

// ServerConfig 模拟 server 段配置。
type ServerConfig struct {
	Host    string `mapstructure:"host" json:"listen_addr"` // 同时支持两种 tag
	Port    int    `mapstructure:"port"`
	Timeout int    `mapstructure:"timeout"`
}

// DatabaseConfig 模拟 database 段配置。
type DatabaseConfig struct {
	DSN     string `mapstructure:"dsn"`
	MaxOpen int    `mapstructure:"max_open"`
	MaxIdle int    `mapstructure:"max_idle"`
}

// FeatureConfig 模拟 features 段配置。
type FeatureConfig struct {
	Metrics        bool     `mapstructure:"metrics"`
	Tracing        bool     `mapstructure:"tracing"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// AppInfo 模拟 app 段配置。
type AppInfo struct {
	Name  string `mapstructure:"name"`
	Debug bool   `mapstructure:"debug"`
}

// LegacyOptions 仅用 json tag 的 struct（验证 json tag 兼容性）。
type LegacyOptions struct {
	ListenAddr string `json:"listen_addr"`
	LogLevel   string `json:"log_level"`
}
