package auth

import "context"

// Storage 权限数据的持久化存储接口。
//
// 所有方法中的 domain 参数用于多租户隔离，空字符串 "" 表示全局（无租户）。
type Storage interface {
	// ---- 主体-角色关系 ----

	// AddRoleForSubject 为主体分配一个全局角色。
	AddRoleForSubject(ctx context.Context, subjectID, roleName string) error

	// RemoveRoleForSubject 移除主体的一个全局角色。
	RemoveRoleForSubject(ctx context.Context, subjectID, roleName string) error

	// GetRolesForSubject 查询主体的所有全局角色。
	GetRolesForSubject(ctx context.Context, subjectID string) ([]string, error)

	// ---- 多租户：主体-角色关系 ----

	// AddRoleForSubjectInDomain 为主体在指定租户域内分配角色。
	AddRoleForSubjectInDomain(ctx context.Context, subjectID, roleName, domain string) error

	// RemoveRoleForSubjectInDomain 移除主体在指定租户域内的角色。
	RemoveRoleForSubjectInDomain(ctx context.Context, subjectID, roleName, domain string) error

	// GetRolesForSubjectInDomain 查询主体在指定租户域内的所有角色。
	GetRolesForSubjectInDomain(ctx context.Context, subjectID, domain string) ([]string, error)

	// ListDomainsForSubject 查询主体拥有的所有租户域（去重）。
	// 返回的 domain 列表包含主体有角色分配的租户域，不包含全局域 ""。
	ListDomainsForSubject(ctx context.Context, subjectID string) ([]string, error)

	// ---- 角色-权限关系 ----

	// AddPermission 为一个全局角色添加权限。
	AddPermission(ctx context.Context, roleName, resource, action string) error

	// RemovePermission 移除全局角色的一个权限。
	RemovePermission(ctx context.Context, roleName, resource, action string) error

	// GetPermissions 查询全局角色的所有权限。
	GetPermissions(ctx context.Context, roleName string) ([]Permission, error)

	// ---- 多租户：角色-权限关系 ----

	// AddPermissionInDomain 为指定租户域内的角色添加权限。
	AddPermissionInDomain(ctx context.Context, roleName, resource, action, domain string) error

	// RemovePermissionInDomain 移除指定租户域内角色的一个权限。
	RemovePermissionInDomain(ctx context.Context, roleName, resource, action, domain string) error

	// GetPermissionsInDomain 查询指定租户域内角色的所有权限。
	GetPermissionsInDomain(ctx context.Context, roleName, domain string) ([]Permission, error)
}

// StorageBulk 提供批量加载/保存能力，用于初始化或备份恢复。
type StorageBulk interface {
	Storage

	// LoadAll 一次性加载所有主体角色关系和角色权限关系。
	// 返回两个 map：roleMembers[subjectID] → []roleName，rolePerms[roleName] → []Permission。
	LoadAll(ctx context.Context) (roleMembers map[string][]string, rolePerms map[string][]Permission, err error)

	// SaveAll 批量写入主体角色关系和角色权限关系（覆盖已有数据）。
	SaveAll(ctx context.Context, roleMembers map[string][]string, rolePerms map[string][]Permission) error
}
