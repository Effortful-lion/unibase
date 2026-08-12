//go:build ignore

package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Effortful-lion/unibase/component/rbac"
	"github.com/Effortful-lion/unibase/logx"
	"github.com/gin-gonic/gin"
)

func main() {
	logx.Init(logx.Config{Level: "info", Format: "console"})

	// ==================== 存储选择 ====================
	// 方式一：内存存储（适合测试和单进程）
	storage := auth.NewMemoryStorage()

	// 方式二：Redis 存储（适合生产环境）
	// import "github.com/redis/go-redis/v9"
	// client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	// storage := auth.NewRedisStorage(auth.RedisClientAdapter(client),
	//     auth.WithRedisPrefix("myapp"),
	//     auth.WithRedisTTL(7*24*time.Hour),
	// )

	// ==================== 创建 Enforcer ====================
	enforcer, err := auth.NewEnforcer(storage,
		auth.WithLogger(logx.Module("auth")),
	)
	if err != nil {
		panic(err)
	}

	// ==================== 定义权限策略 ====================
	ctx := context.Background()

	// admin 角色：全部权限
	enforcer.AddPermission(ctx, "admin", "*", "*")

	// editor 角色：读写 post
	enforcer.AddPermission(ctx, "editor", "post", "read")
	enforcer.AddPermission(ctx, "editor", "post", "write")

	// viewer 角色：只能读 post
	enforcer.AddPermission(ctx, "viewer", "post", "read")

	// ==================== 分配角色 ====================
	enforcer.AddRoleForSubject(ctx, "user-001", "admin")
	enforcer.AddRoleForSubject(ctx, "user-002", "editor")
	enforcer.AddRoleForSubject(ctx, "user-003", "viewer")

	// ==================== 多租户示例 ====================
	// org-a 租户内的 editor 只能读
	enforcer.AddPermissionInDomain(ctx, "editor", "post", "read", "org-a")
	enforcer.AddRoleForSubjectInDomain(ctx, "user-004", "editor", "org-a")

	// org-b 租户内的 editor 可以读写
	enforcer.AddPermissionInDomain(ctx, "editor", "post", "read", "org-b")
	enforcer.AddPermissionInDomain(ctx, "editor", "post", "write", "org-b")
	enforcer.AddRoleForSubjectInDomain(ctx, "user-005", "editor", "org-b")

	// ==================== 权限检查示例 ====================
	fmt.Println("=== 权限检查 ===")

	check := func(subjectID, resource, action, domain string) {
		var allowed bool
		var err error
		if domain != "" {
			allowed, err = enforcer.IsAllowedInDomain(ctx, subjectID, resource, action, domain)
		} else {
			allowed, err = enforcer.IsAllowed(ctx, subjectID, resource, action)
		}
		status := "DENIED"
		if allowed {
			status = "ALLOWED"
		}
		if err != nil {
			status = "ERROR: " + err.Error()
		}
		domainStr := ""
		if domain != "" {
			domainStr = fmt.Sprintf(" [domain=%s]", domain)
		}
		fmt.Printf("  %s can %s %s%s → %s\n", subjectID, action, resource, domainStr, status)
	}

	// 全局权限检查
	check("user-001", "post", "read", "")   // admin: ALLOWED (wildcard)
	check("user-001", "user", "delete", "") // admin: ALLOWED (wildcard)
	check("user-002", "post", "write", "")  // editor: ALLOWED
	check("user-002", "user", "read", "")   // editor: DENIED (no permission)
	check("user-003", "post", "read", "")   // viewer: ALLOWED
	check("user-003", "post", "write", "")  // viewer: DENIED

	// 多租户权限检查
	check("user-004", "post", "read", "org-a") // DENIED (org-a editor can only read... wait let me check)
	// Actually user-004 has editor role in org-a with only read permission on post
	check("user-004", "post", "read", "org-a")  // ALLOWED
	check("user-004", "post", "write", "org-a") // DENIED (no write in org-a)
	check("user-005", "post", "write", "org-b") // ALLOWED

	// ==================== Gin HTTP 服务 ====================
	r := gin.Default()

	// 健康检查（白名单）
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 受保护的路由
	api := r.Group("/api")
	{
		api.Use(auth.Middleware(enforcer,
			auth.WithSkipPaths([]string{"/api/public"}),
		))
		api.GET("/posts", listPosts)
		api.POST("/posts", createPost)
	}

	fmt.Println("\n=== 服务启动在 :8080 ===")
	fmt.Println("curl -H 'Authorization: Bearer <token-with-role=editor>' http://localhost:8080/api/posts")
	r.Run(":8080")
}

func listPosts(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"posts": []string{"post-1", "post-2"},
	})
}

func createPost(c *gin.Context) {
	c.JSON(http.StatusCreated, gin.H{
		"message": "post created",
	})
}
