package auth

import (
	"context"
	"sync"
)

// MemoryStorage 内存存储实现，适合测试和单进程场景。
//
// 数据结构：
//   - roleMembers:     domain → roleName → set<subjectID>
//   - rolePermissions: domain → roleName → set<resource:action>
type MemoryStorage struct {
	mu              sync.RWMutex
	roleMembers     map[string]map[string]map[string]bool // domain → roleName → subjectID
	rolePermissions map[string]map[string]map[string]bool // domain → roleName → "resource:action"
}

// NewMemoryStorage 创建内存存储实例。
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		roleMembers:     make(map[string]map[string]map[string]bool),
		rolePermissions: make(map[string]map[string]map[string]bool),
	}
}

// ---------- 辅助方法 ----------

func (m *MemoryStorage) ensureRoleMembers(domain string) map[string]map[string]bool {
	if _, ok := m.roleMembers[domain]; !ok {
		m.roleMembers[domain] = make(map[string]map[string]bool)
	}
	return m.roleMembers[domain]
}

func (m *MemoryStorage) ensureRolePermissions(domain string) map[string]map[string]bool {
	if _, ok := m.rolePermissions[domain]; !ok {
		m.rolePermissions[domain] = make(map[string]map[string]bool)
	}
	return m.rolePermissions[domain]
}

func (m *MemoryStorage) ensureRoleMembersSet(domain, roleName string) map[string]bool {
	roles := m.ensureRoleMembers(domain)
	if _, ok := roles[roleName]; !ok {
		roles[roleName] = make(map[string]bool)
	}
	return roles[roleName]
}

func (m *MemoryStorage) ensureRolePermsSet(domain, roleName string) map[string]bool {
	perms := m.ensureRolePermissions(domain)
	if _, ok := perms[roleName]; !ok {
		perms[roleName] = make(map[string]bool)
	}
	return perms[roleName]
}

// ---------- Subject ↔ Role（全局）----------

func (m *MemoryStorage) AddRoleForSubject(_ context.Context, subjectID, roleName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRoleMembersSet("", roleName)[subjectID] = true
	return nil
}

func (m *MemoryStorage) RemoveRoleForSubject(_ context.Context, subjectID, roleName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if roles, ok := m.roleMembers[""][roleName]; ok {
		delete(roles, subjectID)
	}
	return nil
}

func (m *MemoryStorage) GetRolesForSubject(_ context.Context, subjectID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	roles, ok := m.roleMembers[""]
	if !ok {
		return nil, nil
	}
	var result []string
	for roleName, members := range roles {
		if members[subjectID] {
			result = append(result, roleName)
		}
	}
	return result, nil
}

// ListDomainsForSubject 查询主体拥有的所有租户域（去重）。
func (m *MemoryStorage) ListDomainsForSubject(_ context.Context, subjectID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	domainSet := make(map[string]bool)
	for domain, roles := range m.roleMembers {
		if domain == "" {
			continue // 跳过全局域
		}
		for _, members := range roles {
			if members[subjectID] {
				domainSet[domain] = true
				break
			}
		}
	}
	domains := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}
	return domains, nil
}

// ---------- Subject ↔ Role（多租户）----------

func (m *MemoryStorage) AddRoleForSubjectInDomain(_ context.Context, subjectID, roleName, domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRoleMembersSet(domain, roleName)[subjectID] = true
	return nil
}

func (m *MemoryStorage) RemoveRoleForSubjectInDomain(_ context.Context, subjectID, roleName, domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if roles, ok := m.roleMembers[domain]; ok {
		if members, ok := roles[roleName]; ok {
			delete(members, subjectID)
		}
	}
	return nil
}

func (m *MemoryStorage) GetRolesForSubjectInDomain(_ context.Context, subjectID, domain string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	roles, ok := m.roleMembers[domain]
	if !ok {
		return nil, nil
	}
	var result []string
	for roleName, members := range roles {
		if members[subjectID] {
			result = append(result, roleName)
		}
	}
	return result, nil
}

// ---------- Role ↔ Permission（全局）----------

func (m *MemoryStorage) AddPermission(_ context.Context, roleName, resource, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRolePermsSet("", roleName)[resource+":"+action] = true
	return nil
}

func (m *MemoryStorage) RemovePermission(_ context.Context, roleName, resource, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if perms, ok := m.rolePermissions[""][roleName]; ok {
		delete(perms, resource+":"+action)
	}
	return nil
}

func (m *MemoryStorage) GetPermissions(_ context.Context, roleName string) ([]Permission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	perms, ok := m.rolePermissions[""]
	if !ok {
		return nil, nil
	}
	set, ok := perms[roleName]
	if !ok {
		return nil, nil
	}
	result := make([]Permission, 0, len(set))
	for p := range set {
		perm, err := ParsePermission(p)
		if err != nil {
			continue
		}
		result = append(result, perm)
	}
	return result, nil
}

// ---------- Role ↔ Permission（多租户）----------

func (m *MemoryStorage) AddPermissionInDomain(_ context.Context, roleName, resource, action, domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureRolePermsSet(domain, roleName)[resource+":"+action] = true
	return nil
}

func (m *MemoryStorage) RemovePermissionInDomain(_ context.Context, roleName, resource, action, domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if perms, ok := m.rolePermissions[domain]; ok {
		if set, ok := perms[roleName]; ok {
			delete(set, resource+":"+action)
		}
	}
	return nil
}

func (m *MemoryStorage) GetPermissionsInDomain(_ context.Context, roleName, domain string) ([]Permission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	perms, ok := m.rolePermissions[domain]
	if !ok {
		return nil, nil
	}
	set, ok := perms[roleName]
	if !ok {
		return nil, nil
	}
	result := make([]Permission, 0, len(set))
	for p := range set {
		perm, err := ParsePermission(p)
		if err != nil {
			continue
		}
		result = append(result, perm)
	}
	return result, nil
}

// ---------- 批量操作（MemoryStorage 直接实现）----------

// LoadAll 从内存中读取所有数据（无外部 I/O）。
func (m *MemoryStorage) LoadAll(_ context.Context) (map[string][]string, map[string][]Permission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	roleMembers := make(map[string][]string)
	for domain, roles := range m.roleMembers {
		prefix := domain
		for roleName, members := range roles {
			for subjectID := range members {
				key := roleName
				if prefix != "" {
					key = prefix + ":" + roleName
				}
				roleMembers[subjectID] = append(roleMembers[subjectID], key)
			}
		}
	}

	rolePerms := make(map[string][]Permission)
	for domain, roles := range m.rolePermissions {
		for roleName, perms := range roles {
			key := roleName
			if domain != "" {
				key = domain + ":" + roleName
			}
			for pStr := range perms {
				perm, err := ParsePermission(pStr)
				if err != nil {
					continue
				}
				rolePerms[key] = append(rolePerms[key], perm)
			}
		}
	}

	return roleMembers, rolePerms, nil
}

// SaveAll 将数据写入内存（覆盖已有）。
func (m *MemoryStorage) SaveAll(_ context.Context, roleMembers map[string][]string, rolePerms map[string][]Permission) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.roleMembers = make(map[string]map[string]map[string]bool)
	m.rolePermissions = make(map[string]map[string]map[string]bool)

	for subjectID, roles := range roleMembers {
		for _, roleKey := range roles {
			domain, roleName := splitRoleKey(roleKey)
			m.ensureRoleMembersSet(domain, roleName)[subjectID] = true
		}
	}

	for roleKey, perms := range rolePerms {
		domain, roleName := splitRoleKey(roleKey)
		for _, perm := range perms {
			m.ensureRolePermsSet(domain, roleName)[perm.String()] = true
		}
	}

	return nil
}

// Ensure MemoryStorage implements Storage.
var _ Storage = (*MemoryStorage)(nil)
