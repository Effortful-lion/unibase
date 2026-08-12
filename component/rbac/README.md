# RBAC

轻量级基于角色的权限认证（RBAC）框架，支持多租户域隔离和 Redis 持久化存储。

## 快速开始

### 1. 定义权限和角色

```go
import "github.com/redis/go-redis/v9"

client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
storage := rbac.NewRedisStorage(
    rbac.RedisClientAdapter(client),
    rbac.WithRedisPrefix("myapp"),
    rbac.WithRedisTTL(7*24*time.Hour),
)

enforcer, _ := rbac.NewEnforcer(storage)

// 定义角色权限
enforcer.AddPermission(ctx, "admin", "*", "*")        // admin: 全部权限
enforcer.AddPermission(ctx, "editor", "post", "read")  // editor: 读 post
enforcer.AddPermission(ctx, "editor", "post", "write") // editor: 写 post
enforcer.AddPermission(ctx, "viewer", "post", "read")  // viewer: 只读 post

// 分配角色
enforcer.AddRoleForSubject(ctx, "user-001", "admin")
enforcer.AddRoleForSubject(ctx, "user-002", "editor")
```

### 2. 权限检查

```go
allowed, _ := enforcer.IsAllowed(ctx, "user-001", "post", "read")  // true
allowed, _ = enforcer.IsAllowed(ctx, "user-003", "post", "write") // false
```

### 3. Gin 中间件集成

```go
r := gin.Default()

// 健康检查（白名单）
r.GET("/health", func(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{"status": "ok"})
})

// 受保护的路由
api := r.Group("/api")
{
    api.Use(rbac.Middleware(enforcer,
        rbac.WithSkipPath("/health"),
        rbac.WithSkipPaths([]string{"/api/public"}),
    ))
    api.GET("/posts", listPosts)
    api.POST("/posts", createPost)
}
```

## 多租户支持

```go
// 在 org-a 租户内定义角色权限
enforcer.AddPermissionInDomain(ctx, "editor", "post", "read", "org-a")
enforcer.AddRoleForSubjectInDomain(ctx, "user-004", "editor", "org-a")

// 多租户权限检查
allowed, _ := enforcer.IsAllowedInDomain(ctx, "user-004", "post", "read", "org-a")
```

## 存储

### Redis 存储（生产）

```go
import "github.com/redis/go-redis/v9"

client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
storage := rbac.NewRedisStorage(
    rbac.RedisClientAdapter(client),
    rbac.WithRedisPrefix("myapp"),
    rbac.WithRedisTTL(7*24*time.Hour),
)
```

### 内存存储（测试）

```go
storage := rbac.NewMemoryStorage()
```

## 核心 API

### Enforcer 接口

- `AddRoleForSubject(subjectID, roleName)` — 分配全局角色
- `AddRoleForSubjectInDomain(subjectID, roleName, domain)` — 分配租户角色
- `AddPermission(roleName, resource, action)` — 添加全局权限
- `AddPermissionInDomain(roleName, resource, action, domain)` — 添加租户权限
- `IsAllowed(subjectID, resource, action)` — 全局权限检查
- `IsAllowedInDomain(subjectID, resource, action, domain)` — 租户权限检查

### 中间件选项

- `WithSkipPath(path)` — 白名单路径
- `WithDomainExtractor(fn)` — 租户域提取函数
- `WithUnauthorizedHandler(handler)` — 自定义拒绝响应

## 数据模型

```
Subject ──member_of──▶ Role ──has──▶ Permission(resource:action)
                        ↑
                  domain（多租户隔离）
```

## License

MIT
