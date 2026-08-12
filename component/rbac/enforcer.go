package auth

import (
	"context"
	"sort"
)

// enforcer 是 Enforcer 的默认实现。
type enforcer struct {
	storage Storage
	opts    enforcerOptions
}

// NewEnforcer 创建 RBAC 执行器实例。
//
// storage 提供角色和权限的持久化存储（内存/Redis 等）。
// opts 用于配置日志器等可选行为。
func NewEnforcer(storage Storage, opts ...Option) (Enforcer, error) {
	if storage == nil {
		return nil, ErrStorageRequired
	}
	cfg := defaultEnforcerOptions()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &enforcer{
		storage: storage,
		opts:    cfg,
	}, nil
}

// ---------- 角色分配（Subject ↔ Role）----------

// AddRoleForSubject 为主体分配一个全局角色。
func (e *enforcer) AddRoleForSubject(ctx context.Context, subjectID, roleName string) error {
	if subjectID == "" {
		return ErrInvalidSubjectID
	}
	if roleName == "" {
		return ErrInvalidRoleName
	}
	return e.storage.AddRoleForSubject(ctx, subjectID, roleName)
}

// AddRoleForSubjectInDomain 为主体在指定租户域内分配角色。
func (e *enforcer) AddRoleForSubjectInDomain(ctx context.Context, subjectID, roleName, domain string) error {
	if subjectID == "" {
		return ErrInvalidSubjectID
	}
	if roleName == "" {
		return ErrInvalidRoleName
	}
	return e.storage.AddRoleForSubjectInDomain(ctx, subjectID, roleName, domain)
}

// RemoveRoleForSubject 移除主体的一个全局角色。
func (e *enforcer) RemoveRoleForSubject(ctx context.Context, subjectID, roleName string) error {
	if subjectID == "" {
		return ErrInvalidSubjectID
	}
	if roleName == "" {
		return ErrInvalidRoleName
	}
	return e.storage.RemoveRoleForSubject(ctx, subjectID, roleName)
}

// RemoveRoleForSubjectInDomain 移除主体在指定租户域内的角色。
func (e *enforcer) RemoveRoleForSubjectInDomain(ctx context.Context, subjectID, roleName, domain string) error {
	if subjectID == "" {
		return ErrInvalidSubjectID
	}
	if roleName == "" {
		return ErrInvalidRoleName
	}
	return e.storage.RemoveRoleForSubjectInDomain(ctx, subjectID, roleName, domain)
}

// GetRolesForSubject 查询主体的所有全局角色。
func (e *enforcer) GetRolesForSubject(ctx context.Context, subjectID string) ([]string, error) {
	if subjectID == "" {
		return nil, ErrInvalidSubjectID
	}
	return e.storage.GetRolesForSubject(ctx, subjectID)
}

// GetRolesForSubjectInDomain 查询主体在指定租户域内的所有角色。
func (e *enforcer) GetRolesForSubjectInDomain(ctx context.Context, subjectID, domain string) ([]string, error) {
	if subjectID == "" {
		return nil, ErrInvalidSubjectID
	}
	return e.storage.GetRolesForSubjectInDomain(ctx, subjectID, domain)
}

// ---------- 权限管理（Role ↔ Permission）----------

// AddPermission 为全局角色添加一个权限。
func (e *enforcer) AddPermission(ctx context.Context, roleName, resource, action string) error {
	if roleName == "" {
		return ErrInvalidRoleName
	}
	if resource == "" || action == "" {
		return ErrInvalidPermission
	}
	return e.storage.AddPermission(ctx, roleName, resource, action)
}

// AddPermissionInDomain 为指定租户域内的角色添加一个权限。
func (e *enforcer) AddPermissionInDomain(ctx context.Context, roleName, resource, action, domain string) error {
	if roleName == "" {
		return ErrInvalidRoleName
	}
	if resource == "" || action == "" {
		return ErrInvalidPermission
	}
	return e.storage.AddPermissionInDomain(ctx, roleName, resource, action, domain)
}

// RemovePermission 移除全局角色的一个权限。
func (e *enforcer) RemovePermission(ctx context.Context, roleName, resource, action string) error {
	if roleName == "" {
		return ErrInvalidRoleName
	}
	if resource == "" || action == "" {
		return ErrInvalidPermission
	}
	return e.storage.RemovePermission(ctx, roleName, resource, action)
}

// RemovePermissionInDomain 移除指定租户域内角色的一个权限。
func (e *enforcer) RemovePermissionInDomain(ctx context.Context, roleName, resource, action, domain string) error {
	if roleName == "" {
		return ErrInvalidRoleName
	}
	if resource == "" || action == "" {
		return ErrInvalidPermission
	}
	return e.storage.RemovePermissionInDomain(ctx, roleName, resource, action, domain)
}

// GetPermissions 查询全局角色的所有权限。
func (e *enforcer) GetPermissions(ctx context.Context, roleName string) ([]Permission, error) {
	if roleName == "" {
		return nil, ErrInvalidRoleName
	}
	return e.storage.GetPermissions(ctx, roleName)
}

// GetPermissionsInDomain 查询指定租户域内角色的所有权限。
func (e *enforcer) GetPermissionsInDomain(ctx context.Context, roleName, domain string) ([]Permission, error) {
	if roleName == "" {
		return nil, ErrInvalidRoleName
	}
	return e.storage.GetPermissionsInDomain(ctx, roleName, domain)
}

// ---------- 权限检查 ----

// IsAllowed 判断主体在全局范围内是否有对 resource:action 的访问权限。
//
// 检查逻辑：
//  1. 获取主体的全局角色 + 所有租户域内的角色
//  2. 对每个角色，获取其全局权限 + 对应租户域内的权限
//  3. 如果任意权限匹配 resource 和 action（支持通配符），返回 true
func (e *enforcer) IsAllowed(ctx context.Context, subjectID, resource, action string) (bool, error) {
	if subjectID == "" {
		return false, ErrInvalidSubjectID
	}
	if resource == "" || action == "" {
		return false, ErrInvalidPermission
	}

	// 获取全局角色
	globalRoles, err := e.storage.GetRolesForSubject(ctx, subjectID)
	if err != nil {
		return false, err
	}

	// 检查全局角色的权限
	for _, roleName := range globalRoles {
		allowed, err := e.checkRolePermission(ctx, roleName, "", resource, action)
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}

	// 检查所有租户域内的角色权限
	domains, err := e.storage.ListDomainsForSubject(ctx, subjectID)
	if err != nil {
		return false, err
	}
	for _, domain := range domains {
		domainRoles, err := e.storage.GetRolesForSubjectInDomain(ctx, subjectID, domain)
		if err != nil {
			return false, err
		}
		for _, roleName := range domainRoles {
			// 检查该角色在租户域内的权限
			allowed, err := e.checkRolePermission(ctx, roleName, domain, resource, action)
			if err != nil {
				return false, err
			}
			if allowed {
				return true, nil
			}
		}
	}

	return false, nil
}

// IsAllowedInDomain 判断主体在指定租户域内是否有对 resource:action 的访问权限。
//
// 检查逻辑：
//  1. 获取主体在该租户域内的角色 + 全局角色
//  2. 对每个角色，获取其在该租户域内的权限 + 全局权限
//  3. 如果任意权限匹配 resource 和 action，返回 true
func (e *enforcer) IsAllowedInDomain(ctx context.Context, subjectID, resource, action, domain string) (bool, error) {
	if subjectID == "" {
		return false, ErrInvalidSubjectID
	}
	if resource == "" || action == "" {
		return false, ErrInvalidPermission
	}

	// 收集需要检查的所有 (roleName, domain) 组合
	roleDomainPairs := make(map[string]string) // roleName → domain

	// 全局角色
	globalRoles, err := e.storage.GetRolesForSubject(ctx, subjectID)
	if err != nil {
		return false, err
	}
	for _, roleName := range globalRoles {
		roleDomainPairs[roleName] = "" // 全局权限
	}

	// 指定租户域内的角色
	domainRoles, err := e.storage.GetRolesForSubjectInDomain(ctx, subjectID, domain)
	if err != nil {
		return false, err
	}
	for _, roleName := range domainRoles {
		roleDomainPairs[roleName] = domain // 租户域权限
	}

	// 对每个角色检查对应域 + 全局域的权限
	for roleName, roleDomain := range roleDomainPairs {
		// 检查该角色在自身域内的权限
		allowed, err := e.checkRolePermission(ctx, roleName, roleDomain, resource, action)
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
		// 如果角色在某个租户域内，还需检查该角色的全局权限
		if roleDomain != "" {
			allowed, err = e.checkRolePermission(ctx, roleName, "", resource, action)
			if err != nil {
				return false, err
			}
			if allowed {
				return true, nil
			}
		}
	}

	return false, nil
}

// checkRolePermission 检查指定角色在指定域内是否拥有匹配 resource:action 的权限。
func (e *enforcer) checkRolePermission(ctx context.Context, roleName, domain, resource, action string) (bool, error) {
	var perms []Permission
	var err error

	if domain == "" {
		perms, err = e.storage.GetPermissions(ctx, roleName)
	} else {
		perms, err = e.storage.GetPermissionsInDomain(ctx, roleName, domain)
	}
	if err != nil {
		return false, err
	}

	for _, perm := range perms {
		if perm.Match(resource, action) {
			return true, nil
		}
	}

	return false, nil
}

// ---------- 批量操作 ----

// Save 将当前权限策略持久化到存储。
func (e *enforcer) Save(ctx context.Context) error {
	bulk, ok := e.storage.(StorageBulk)
	if !ok {
		return ErrStorageRequired
	}

	roleMembers, rolePerms, err := e.collectAllPolicies(ctx)
	if err != nil {
		return err
	}

	return bulk.SaveAll(ctx, roleMembers, rolePerms)
}

// Load 从存储加载所有权限策略。
func (e *enforcer) Load(ctx context.Context) error {
	bulk, ok := e.storage.(StorageBulk)
	if !ok {
		return ErrStorageRequired
	}
	_, _, err := bulk.LoadAll(ctx)
	return err
}

// collectAllPolicies 从存储中收集所有主体角色关系和角色权限关系。
func (e *enforcer) collectAllPolicies(ctx context.Context) (map[string][]string, map[string][]Permission, error) {
	// 注意：这里需要一个能枚举所有主体的方法。
	// 内存存储可以直接遍历，Redis 存储需要额外的 Scan 操作。
	// 目前通过 StorageBulk 接口由具体存储实现决定。
	bulk, ok := e.storage.(StorageBulk)
	if !ok {
		return nil, nil, ErrStorageRequired
	}
	return bulk.LoadAll(ctx)
}

// ---------- 查询辅助 ----

// ListRoles 返回所有已定义的全局角色名（去重）。
func (e *enforcer) ListRoles(ctx context.Context) ([]string, error) {
	bulk, ok := e.storage.(StorageBulk)
	if !ok {
		return nil, nil
	}
	_, rolePerms, err := bulk.LoadAll(ctx)
	if err != nil {
		return nil, err
	}
	roles := make([]string, 0, len(rolePerms))
	for roleName := range rolePerms {
		roles = append(roles, roleName)
	}
	sort.Strings(roles)
	return roles, nil
}

// Ensure enforcer implements Enforcer.
var _ Enforcer = (*enforcer)(nil)
