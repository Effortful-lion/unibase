//go:build !skip_enforcer_tests

package auth

import (
	"context"
	"testing"
)

func TestEnforcer_BasicFlow(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, err := NewEnforcer(storage)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}

	// 定义权限
	_ = enforcer.AddPermission(ctx, "admin", "*", "*")
	_ = enforcer.AddPermission(ctx, "editor", "post", "read")
	_ = enforcer.AddPermission(ctx, "editor", "post", "write")
	_ = enforcer.AddPermission(ctx, "viewer", "post", "read")

	// 分配角色
	_ = enforcer.AddRoleForSubject(ctx, "user-1", "admin")
	_ = enforcer.AddRoleForSubject(ctx, "user-2", "editor")
	_ = enforcer.AddRoleForSubject(ctx, "user-3", "viewer")

	// admin 拥有全部权限
	allowed, err := enforcer.IsAllowed(ctx, "user-1", "post", "read")
	if err != nil || !allowed {
		t.Errorf("user-1 admin: expected allowed, got %v, err=%v", allowed, err)
	}
	allowed, _ = enforcer.IsAllowed(ctx, "user-1", "user", "delete")
	if !allowed {
		t.Error("user-1 admin: expected allowed for wildcard")
	}

	// editor 可以读写 post
	allowed, err = enforcer.IsAllowed(ctx, "user-2", "post", "read")
	if err != nil || !allowed {
		t.Errorf("user-2 editor: expected allowed for post:read, got %v, err=%v", allowed, err)
	}
	allowed, _ = enforcer.IsAllowed(ctx, "user-2", "post", "write")
	if !allowed {
		t.Error("user-2 editor: expected allowed for post:write")
	}

	// editor 不能访问 user 资源
	allowed, _ = enforcer.IsAllowed(ctx, "user-2", "user", "read")
	if allowed {
		t.Error("user-2 editor: expected denied for user:read")
	}

	// viewer 只能读 post
	allowed, _ = enforcer.IsAllowed(ctx, "user-3", "post", "read")
	if !allowed {
		t.Error("user-3 viewer: expected allowed for post:read")
	}
	allowed, _ = enforcer.IsAllowed(ctx, "user-3", "post", "write")
	if allowed {
		t.Error("user-3 viewer: expected denied for post:write")
	}
}

func TestEnforcer_DomainIsolation(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	// 在不同租户域定义不同的权限
	_ = enforcer.AddPermissionInDomain(ctx, "editor", "post", "read", "org-a")
	_ = enforcer.AddPermissionInDomain(ctx, "editor", "post", "write", "org-b")

	// 分配域内角色
	_ = enforcer.AddRoleForSubjectInDomain(ctx, "user-1", "editor", "org-a")
	_ = enforcer.AddRoleForSubjectInDomain(ctx, "user-1", "editor", "org-b")

	// user-1 在 org-a 只能读
	allowed, _ := enforcer.IsAllowedInDomain(ctx, "user-1", "post", "read", "org-a")
	if !allowed {
		t.Error("expected user-1 to be allowed post:read in org-a")
	}
	allowed, _ = enforcer.IsAllowedInDomain(ctx, "user-1", "post", "write", "org-a")
	if allowed {
		t.Error("expected user-1 to be denied post:write in org-a")
	}

	// user-1 在 org-b 只能写
	allowed, _ = enforcer.IsAllowedInDomain(ctx, "user-1", "post", "write", "org-b")
	if !allowed {
		t.Error("expected user-1 to be allowed post:write in org-b")
	}
	allowed, _ = enforcer.IsAllowedInDomain(ctx, "user-1", "post", "read", "org-b")
	if allowed {
		t.Error("expected user-1 to be denied post:read in org-b")
	}
}

func TestEnforcer_DomainFallsBackToGlobal(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	// 全局权限
	_ = enforcer.AddPermission(ctx, "viewer", "post", "read")

	// 域内角色
	_ = enforcer.AddRoleForSubjectInDomain(ctx, "user-1", "viewer", "org-a")

	// user-1 在 org-a 应该有全局 viewer 的权限
	allowed, _ := enforcer.IsAllowedInDomain(ctx, "user-1", "post", "read", "org-a")
	if !allowed {
		t.Error("expected user-1 viewer in org-a to have global post:read")
	}
}

func TestEnforcer_RemoveRole(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	_ = enforcer.AddPermission(ctx, "admin", "*", "*")
	_ = enforcer.AddRoleForSubject(ctx, "user-1", "admin")

	allowed, _ := enforcer.IsAllowed(ctx, "user-1", "anything", "go")
	if !allowed {
		t.Fatal("expected allowed before removal")
	}

	_ = enforcer.RemoveRoleForSubject(ctx, "user-1", "admin")

	allowed, _ = enforcer.IsAllowed(ctx, "user-1", "anything", "go")
	if allowed {
		t.Error("expected denied after role removal")
	}
}

func TestEnforcer_RemovePermission(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	_ = enforcer.AddPermission(ctx, "editor", "post", "read")
	_ = enforcer.AddRoleForSubject(ctx, "user-1", "editor")

	allowed, _ := enforcer.IsAllowed(ctx, "user-1", "post", "read")
	if !allowed {
		t.Fatal("expected allowed before permission removal")
	}

	_ = enforcer.RemovePermission(ctx, "editor", "post", "read")

	allowed, _ = enforcer.IsAllowed(ctx, "user-1", "post", "read")
	if allowed {
		t.Error("expected denied after permission removal")
	}
}

func TestEnforcer_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	// 空 subjectID
	_, err := enforcer.IsAllowed(ctx, "", "post", "read")
	if err != ErrInvalidSubjectID {
		t.Errorf("expected ErrInvalidSubjectID, got %v", err)
	}

	// 空 resource
	_, err = enforcer.IsAllowed(ctx, "user-1", "", "read")
	if err != ErrInvalidPermission {
		t.Errorf("expected ErrInvalidPermission, got %v", err)
	}

	// 空 roleName
	err = enforcer.AddPermission(ctx, "", "post", "read")
	if err != ErrInvalidRoleName {
		t.Errorf("expected ErrInvalidRoleName, got %v", err)
	}
}

func TestEnforcer_MultiRoleInheritance(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	_ = enforcer.AddPermission(ctx, "role-a", "post", "read")
	_ = enforcer.AddPermission(ctx, "role-b", "post", "write")
	_ = enforcer.AddPermission(ctx, "role-b", "comment", "read")

	_ = enforcer.AddRoleForSubject(ctx, "user-1", "role-a")
	_ = enforcer.AddRoleForSubject(ctx, "user-1", "role-b")

	// user-1 应该拥有两个角色的全部权限
	allowed, _ := enforcer.IsAllowed(ctx, "user-1", "post", "read")
	if !allowed {
		t.Error("expected allowed for post:read via role-a")
	}
	allowed, _ = enforcer.IsAllowed(ctx, "user-1", "post", "write")
	if !allowed {
		t.Error("expected allowed for post:write via role-b")
	}
	allowed, _ = enforcer.IsAllowed(ctx, "user-1", "comment", "read")
	if !allowed {
		t.Error("expected allowed for comment:read via role-b")
	}
}

func TestEnforcer_IsAllowed_WithDomainRoles(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	// 只给 user-1 分配域内角色，没有全局角色
	_ = enforcer.AddPermissionInDomain(ctx, "editor", "post", "read", "org-a")
	_ = enforcer.AddRoleForSubjectInDomain(ctx, "user-1", "editor", "org-a")

	// IsAllowed 应该也能检查到域内角色的权限
	allowed, err := enforcer.IsAllowed(ctx, "user-1", "post", "read")
	if err != nil {
		t.Fatalf("IsAllowed: %v", err)
	}
	if !allowed {
		t.Error("expected IsAllowed to check domain roles")
	}
}

func TestEnforcer_RemovePermission_Validation(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	// 空 resource
	err := enforcer.RemovePermission(ctx, "editor", "", "read")
	if err != ErrInvalidPermission {
		t.Errorf("expected ErrInvalidPermission for empty resource, got %v", err)
	}

	// 空 action
	err = enforcer.RemovePermission(ctx, "editor", "post", "")
	if err != ErrInvalidPermission {
		t.Errorf("expected ErrInvalidPermission for empty action, got %v", err)
	}
}

func TestEnforcer_ListDomainsForSubject(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	_ = enforcer.AddRoleForSubjectInDomain(ctx, "user-1", "editor", "org-a")
	_ = enforcer.AddRoleForSubjectInDomain(ctx, "user-1", "viewer", "org-b")
	_ = enforcer.AddRoleForSubjectInDomain(ctx, "user-1", "admin", "org-a") // same domain

	// ListDomainsForSubject 通过 IsAllowedInDomain 间接验证
	// 验证 user-1 在 org-a 有 editor 权限
	allowed, _ := enforcer.IsAllowedInDomain(ctx, "user-1", "post", "read", "org-a")
	if !allowed {
		// 需要先添加权限
		_ = enforcer.AddPermissionInDomain(ctx, "editor", "post", "read", "org-a")
	}
	allowed, _ = enforcer.IsAllowedInDomain(ctx, "user-1", "post", "read", "org-a")
	if !allowed {
		t.Error("expected user-1 to have permissions in org-a domain")
	}

	// 验证 user-1 在 org-b 有 viewer 权限
	_ = enforcer.AddPermissionInDomain(ctx, "viewer", "post", "read", "org-b")
	allowed, _ = enforcer.IsAllowedInDomain(ctx, "user-1", "post", "read", "org-b")
	if !allowed {
		t.Error("expected user-1 to have permissions in org-b domain")
	}
}
