package auth

import (
	"context"
	"strings"
)

// Permission 表示一个权限，由 resource（资源）和 action（操作）组成。
//
// 权限字符串格式为 "resource:action"，例如 "user:read"、"order:delete"。
// 通配符 "*" 可以匹配任意 resource 或 action。
type Permission struct {
	Resource string
	Action   string
}

// String 返回权限的标准字符串表示 "resource:action"。
func (p Permission) String() string {
	return p.Resource + ":" + p.Action
}

// Match 判断该权限是否匹配给定的 resource 和 action。
// 支持通配符 "*" 匹配任意值。
func (p Permission) Match(resource, action string) bool {
	if p.Resource != "*" && p.Resource != resource {
		return false
	}
	if p.Action != "*" && p.Action != action {
		return false
	}
	return true
}

// Equals 判断两个权限是否相等。
func (p Permission) Equals(other Permission) bool {
	return p.Resource == other.Resource && p.Action == other.Action
}

// Subject 表示一个权限主体（用户、服务账号等）。
type Subject struct {
	ID     string
	Name   string
	Domain string // 所属租户域，空字符串表示全局
}

// Role 表示一个角色。
type Role struct {
	Name   string
	Domain string // 所属租户域，空字符串表示全局
}

// Policy 表示一条角色-权限策略。
type Policy struct {
	Role   Role
	Effect Effect
	Perm   Permission
}

// Enforcer 是 RBAC 执行器接口，提供角色管理和权限检查的核心能力。
type Enforcer interface {
	// ---- 角色分配（Subject ↔ Role）----
	AddRoleForSubject(ctx context.Context, subjectID, roleName string) error
	AddRoleForSubjectInDomain(ctx context.Context, subjectID, roleName, domain string) error
	RemoveRoleForSubject(ctx context.Context, subjectID, roleName string) error
	RemoveRoleForSubjectInDomain(ctx context.Context, subjectID, roleName, domain string) error
	GetRolesForSubject(ctx context.Context, subjectID string) ([]string, error)
	GetRolesForSubjectInDomain(ctx context.Context, subjectID, domain string) ([]string, error)

	// ---- 权限管理（Role ↔ Permission）----
	AddPermission(ctx context.Context, roleName, resource, action string) error
	AddPermissionInDomain(ctx context.Context, roleName, resource, action, domain string) error
	RemovePermission(ctx context.Context, roleName, resource, action string) error
	RemovePermissionInDomain(ctx context.Context, roleName, resource, action, domain string) error
	GetPermissions(ctx context.Context, roleName string) ([]Permission, error)
	GetPermissionsInDomain(ctx context.Context, roleName, domain string) ([]Permission, error)

	// ---- 权限检查 ----
	IsAllowed(ctx context.Context, subjectID, resource, action string) (bool, error)
	IsAllowedInDomain(ctx context.Context, subjectID, resource, action, domain string) (bool, error)
}

// Effect 表示权限效果。
type Effect string

const (
	// EffectAllow 允许访问。
	EffectAllow Effect = "allow"
	// EffectDeny 拒绝访问。
	EffectDeny Effect = "deny"
)

// ParsePermissions 将 "resource:action" 字符串数组解析为 Permission 列表。
// 遇到无效格式的条目时跳过，不返回错误。
func ParsePermissions(members []string) ([]Permission, error) {
	result := make([]Permission, 0, len(members))
	for _, m := range members {
		perm, err := ParsePermission(m)
		if err != nil {
			continue
		}
		result = append(result, perm)
	}
	return result, nil
}

// ParsePermission 将 "resource:action" 字符串解析为 Permission。
func ParsePermission(s string) (Permission, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Permission{}, ErrInvalidPermission
	}
	return Permission{Resource: parts[0], Action: parts[1]}, nil
}

// splitRoleKey 将 role key 拆分为 (domain, roleName)。
// 格式：无 domain 时为 "roleName"，有 domain 时为 "domain:roleName"。
func splitRoleKey(key string) (domain, roleName string) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", key
}
