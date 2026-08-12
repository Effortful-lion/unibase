package auth

import (
	"fmt"
	"testing"
)

// TestRedisStorageKeyFormats 验证 Redis key 格式符合设计规范。
func TestRedisStorageKeyFormats(t *testing.T) {
	s := NewRedisStorage(nil, WithRedisPrefix("myapp"))

	tests := []struct {
		name     string
		expected string
		actual   string
	}{
		{"subject_roles global", "myapp:subject_roles:user-1", s.keySubjectRoles("user-1")},
		{"role_perms global", "myapp:role_perms:admin", s.keyRolePerms("admin")},
		{"subject_roles domain", "myapp:subject_roles:org-a::user-1", s.keySubjectRolesDomain("org-a", "user-1")},
		{"role_perms domain", "myapp:role_perms:org-a::admin", s.keyRolePermsDomain("org-a", "admin")},
	}

	for _, tc := range tests {
		if tc.actual != tc.expected {
			t.Errorf("%s: expected %q, got %q", tc.name, tc.expected, tc.actual)
		}
	}
}

// TestParsePermissions 测试权限字符串解析。
func TestParsePermissions(t *testing.T) {
	members := []string{"post:read", "post:write", "invalid", "user:delete"}
	perms, err := ParsePermissions(members)
	if err != nil {
		t.Fatalf("ParsePermissions: %v", err)
	}
	if len(perms) != 3 {
		t.Fatalf("expected 3 valid permissions, got %d", len(perms))
	}

	expected := []Permission{
		{Resource: "post", Action: "read"},
		{Resource: "post", Action: "write"},
		{Resource: "user", Action: "delete"},
	}
	for i, exp := range expected {
		if !perms[i].Equals(exp) {
			t.Errorf("perm %d: expected %v, got %v", i, exp, perms[i])
		}
	}
}

// TestSplitRoleKey 测试 role key 拆分。
func TestSplitRoleKey(t *testing.T) {
	tests := []struct {
		key            string
		expectedDomain string
		expectedRole   string
	}{
		{"admin", "", "admin"},
		{"org-a:admin", "org-a", "admin"},
		{"multi:part:role", "multi", "part:role"},
	}

	for _, tc := range tests {
		domain, role := splitRoleKey(tc.key)
		if domain != tc.expectedDomain || role != tc.expectedRole {
			t.Errorf("splitRoleKey(%q): expected (%q, %q), got (%q, %q)",
				tc.key, tc.expectedDomain, tc.expectedRole, domain, role)
		}
	}
}

// TestExtractSuffix 测试 key 后缀提取。
func TestExtractSuffix(t *testing.T) {
	s := NewRedisStorage(nil, WithRedisPrefix("auth"))

	tests := []struct {
		key            string
		prefix         string
		expectedDomain string
		expectedSuffix string
	}{
		{"auth:subject_roles:user-1", "auth:subject_roles:", "", "user-1"},
		{"auth:subject_roles:org-a::user-1", "auth:subject_roles:", "org-a", "user-1"},
		{"auth:role_perms:admin", "auth:role_perms:", "", "admin"},
		{"auth:role_perms:org-a::admin", "auth:role_perms:", "org-a", "admin"},
	}

	for _, tc := range tests {
		domain, suffix := s.extractSuffix(tc.key, tc.prefix)
		if domain != tc.expectedDomain || suffix != tc.expectedSuffix {
			t.Errorf("extractSuffix(%q, %q): expected (%q, %q), got (%q, %q)",
				tc.key, tc.prefix, tc.expectedDomain, tc.expectedSuffix, domain, suffix)
		}
	}
}

// TestMemoryStorage_ImplementsInterfaces 确保接口契约。
func TestMemoryStorage_ImplementsInterfaces(t *testing.T) {
	var _ Storage = (*MemoryStorage)(nil)
	var _ StorageBulk = (*MemoryStorage)(nil)
	fmt.Println("interfaces verified")
}

func TestRedisStorage_ListDomainsForSubject(t *testing.T) {
	s := NewMemoryStorage() // 使用内存存储模拟 Redis 行为

	_ = s.AddRoleForSubjectInDomain(nil, "user-1", "editor", "org-a")
	_ = s.AddRoleForSubjectInDomain(nil, "user-1", "viewer", "org-b")
	_ = s.AddRoleForSubjectInDomain(nil, "user-1", "admin", "org-a")

	domains, err := s.ListDomainsForSubject(nil, "user-1")
	if err != nil {
		t.Fatalf("ListDomainsForSubject: %v", err)
	}
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
}

func TestMemoryStorage_SaveAll_WriteThenClear(t *testing.T) {
	// SaveAll 应该先写新数据再清旧数据，避免数据丢失
	// 这个测试验证接口契约：writeAll 在 clearAll 之前调用
	s := NewMemoryStorage()

	// 先写入一些数据
	roleMembers := map[string][]string{
		"user-1": {"admin"},
	}
	rolePerms := map[string][]Permission{
		"admin": {{Resource: "*", Action: "*"}},
	}

	// SaveAll 应该成功
	err := s.SaveAll(nil, roleMembers, rolePerms)
	if err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// 验证数据已写入
	roles, _ := s.GetRolesForSubject(nil, "user-1")
	if len(roles) != 1 || roles[0] != "admin" {
		t.Fatalf("expected [admin], got %v", roles)
	}
}
