/*
Package auth 提供轻量级基于角色的权限认证（RBAC）框架。

核心概念：

	Subject  ──member_of──▶  Role  ──has──▶  Permission(resource:action)

	Domain 用于多租户隔离，同一租户内的角色和权限相互独立。

典型用法：

	storage := auth.NewRedisStorage(redisClient, auth.WithRedisPrefix("myapp"))
	enforcer, err := auth.NewEnforcer(storage)

	// 定义权限
	enforcer.AddPermission("admin", "*", "*")
	enforcer.AddPermission("editor", "post", "read")
	enforcer.AddPermission("editor", "post", "write")
	enforcer.AddPermission("viewer", "post", "read")

	// 分配角色
	enforcer.AddRoleForSubject("user-001", "admin")
	enforcer.AddRoleForSubjectInDomain("user-002", "editor", "org-001")

	// 权限检查
	allowed, _ := enforcer.IsAllowed("user-001", "post", "read") // true

与 httpx JWT 中间件集成：

	r.GET("/api/posts", auth.Middleware(enforcer), listHandler)
*/
package auth
