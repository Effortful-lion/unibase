# cache

多级缓存模块，提供本地 LRU 缓存和远程 Redis 缓存两层能力。

## 架构总览

```
┌──────────────────────────────────────────────┐
│  RedisCache（远程 Redis 缓存）                │
│  - singleflight 防击穿                         │
│  - 空值缓存防穿透                              │
│  - 可选本地 LRU 二级回退                       │
│  - JSON 自动序列化                             │
├──────────────────────────────────────────────┤
│  LRUCache（本地 LRU 缓存）                    │
│  - TTL 过期（per-entry time.Timer）           │
│  - 淘汰回调                                    │
│  - 命中率统计                                  │
│  - singleflight 防并发计算                     │
└──────────────────────────────────────────────┘
```

## 文件结构

| 文件 | 职责 |
|---|---|
| `local_lru.go` | 本地 LRU 缓存，带 TTL、淘汰回调、命中率统计 |
| `remote_redis.go` | 远程 Redis 缓存，支持本地回退、单飞、空值缓存 |
| `doc.go` | 包说明 |

## 核心设计

### 本地缓存（LRUCache）

- **淘汰策略**：包装 `hashicorp/golang-lru/v2`，线程安全，支持泛型
- **TTL 过期**：每个 entry 独立 `time.Timer`，过期自动删除；LRU 淘汰时 timer 同步停止
- **命中率统计**：`atomic.Uint64` 计数器，`HitRate()` 返回百分比
- **并发安全**：`sync.RWMutex` 保护所有操作
- **关闭安全**：`Close()` 停止所有 timer，`closed` 标志阻止关闭后写入

### 远程缓存（RedisCache）

- **客户端兼容**：接受 `redis.Cmdable` 接口，单节点/集群/Sentinel 透明适配
- **单飞防击穿**：`singleflight.Group` 合并同一 key 的并发 Redis 请求
- **空值缓存**：`WithCacheEmptyValue()` 选项，缓存穿透时存入 sentinel，返回 `ErrIsEmptyValue`
- **本地回退**：可选 `LRUCache` 作为二级缓存，TTL = Redis TTL × ratio（默认 0.5）
- **序列化**：JSON marshal/unmarshal，通用但非最优

### 依赖

```
github.com/hashicorp/golang-lru/v2  v2.0.7  # LRU 淘汰
github.com/redis/go-redis/v9       v9.22.0  # Redis 客户端
golang.org/x/sync                  v0.22.0  # singleflight 防击穿
```

## API

### LRUCache

```go
// 创建
c, err := NewLRUCache(LRUConfig{MaxEntries: 1000})

// 基础操作
c.Get(key)           // (value, ok)
c.Put(key, value, ttl)
c.Remove(key)        // (value, ok)
c.Contains(key)      // bool
c.Len()              // int

// 读取或计算（singleflight 保护）
val, err := c.GetOrSet(key, func() (any, error) { ... }, ttl)

// 批量操作
c.MGet(keys, values) // []bool
c.MSet(items, ttl)

// 元信息
rate, length := c.HitRate()
c.Keys()            // []string
c.Iterator(func(k string, v any) bool { ... })

// 生命周期
c.Flush()           // 清空所有
c.Close()           // 停止 timer，阻止后续写入
```

### RedisCache

```go
// 创建
c := NewRedisCache(client, WithLocalCache(local), WithCacheEmptyValue())

// 基础操作
c.Put(ctx, key, value, ttl)
c.Get(ctx, key, &value)    // (ok, err)
c.Remove(ctx, key)         // err

// 读取或计算（singleflight 保护）
val, err := c.GetOrSet(ctx, key, func() (any, error) { ... }, ttl)

// 批量操作（Redis 用 Pipeline）
c.MGet(ctx, keys, values)  // ([]bool, err)
c.MSet(ctx, items, ttl)    // err

// 元信息
hr := c.HitRate()          // {RedisHitRate int}
localRate, _ := c.LocalHitRate()

// 本地缓存管理
c.FlushLocal()             // 清空本地缓存（如有）
```

## 使用示例

### 仅本地缓存

```go
local, _ := cache.NewLRUCache(cache.LRUConfig{MaxEntries: 1000})
defer local.Close()

val, _ := local.Get("user:123")
local.Put("user:123", user, 5*time.Minute)
```

### 仅 Redis

```go
redisCache := cache.NewRedisCache(redisClient)
defer redisCache.FlushLocal()

var user User
ok, err := redisCache.Get(ctx, "user:123", &user)
```

### 本地 + Redis 二级缓存

```go
local, _ := cache.NewLRUCache(cache.LRUConfig{MaxEntries: 1000})
defer local.Close()

redisCache := cache.NewRedisCache(redisClient,
    cache.WithLocalCache(local),
    cache.WithCacheEmptyValue(),
)

// GetOrSet 自动处理缓存未命中
val, err := redisCache.GetOrSet(ctx, "user:123", func() (any, error) {
    return loadUserFromDB(ctx, "123")
}, 5*time.Minute)
```

## 技术选型说明

| 能力 | 方案 | 理由 |
|---|---|---|
| LRU 淘汰 | `hashicorp/golang-lru/v2` | 最成熟的 Go LRU 库，v2 支持泛型，线程安全 |
| TTL 过期 | `time.Timer` per entry | 简单可靠，无轮询开销；万级条目以下无压力 |
| 防击穿 | `singleflight.Group` | 标准库，合并同一 key 的并发请求 |
| 防穿透 | 空值 sentinel | 简单有效，`ErrIsEmptyValue` 可区分 |
| 序列化 | JSON | 通用性强，调试友好；后续可替换 |
| 批量操作 | Pipeline（Redis）/ 循环（LRU） | Pipeline 一次网络往返；LRU 内存操作无网络开销 |
