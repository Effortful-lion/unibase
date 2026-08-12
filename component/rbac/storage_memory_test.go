package auth

import (
	"context"
	"testing"
)

func TestMemoryStorage_AddRoleForSubject(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()

	if err := s.AddRoleForSubject(ctx, "user-1", "admin"); err != nil {
		t.Fatalf("AddRoleForSubject: %v", err)
	}
	roles, err := s.GetRolesForSubject(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetRolesForSubject: %v", err)
	}
	if len(roles) != 1 || roles[0] != "admin" {
		t.Fatalf("expected [admin], got %v", roles)
	}
}

func TestMemoryStorage_RemoveRoleForSubject(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()

	_ = s.AddRoleForSubject(ctx, "user-1", "admin")
	_ = s.RemoveRoleForSubject(ctx, "user-1", "admin")

	roles, _ := s.GetRolesForSubject(ctx, "user-1")
	if len(roles) != 0 {
		t.Fatalf("expected [], got %v", roles)
	}
}

func TestMemoryStorage_MultiRoles(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()

	_ = s.AddRoleForSubject(ctx, "user-1", "admin")
	_ = s.AddRoleForSubject(ctx, "user-1", "editor")

	roles, _ := s.GetRolesForSubject(ctx, "user-1")
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %d: %v", len(roles), roles)
	}
}

func TestMemoryStorage_DomainIsolation(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()

	_ = s.AddRoleForSubjectInDomain(ctx, "user-1", "editor", "org-a")
	_ = s.AddRoleForSubjectInDomain(ctx, "user-1", "viewer", "org-b")

	rolesA, _ := s.GetRolesForSubjectInDomain(ctx, "user-1", "org-a")
	if len(rolesA) != 1 || rolesA[0] != "editor" {
		t.Fatalf("org-a: expected [editor], got %v", rolesA)
	}

	rolesB, _ := s.GetRolesForSubjectInDomain(ctx, "user-1", "org-b")
	if len(rolesB) != 1 || rolesB[0] != "viewer" {
		t.Fatalf("org-b: expected [viewer], got %v", rolesB)
	}

	// 全局角色不应受 domain 影响
	rolesGlobal, _ := s.GetRolesForSubject(ctx, "user-1")
	if len(rolesGlobal) != 0 {
		t.Fatalf("global: expected [], got %v", rolesGlobal)
	}
}

func TestMemoryStorage_Permissions(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()

	_ = s.AddPermission(ctx, "admin", "*", "*")
	_ = s.AddPermission(ctx, "editor", "post", "read")
	_ = s.AddPermission(ctx, "editor", "post", "write")

	perms, _ := s.GetPermissions(ctx, "admin")
	if len(perms) != 1 || !perms[0].Match("*", "*") {
		t.Fatalf("admin perms: expected wildcard, got %v", perms)
	}

	perms, _ = s.GetPermissions(ctx, "editor")
	if len(perms) != 2 {
		t.Fatalf("editor perms: expected 2, got %d: %v", len(perms), perms)
	}

	_ = s.RemovePermission(ctx, "editor", "post", "write")
	perms, _ = s.GetPermissions(ctx, "editor")
	if len(perms) != 1 {
		t.Fatalf("editor perms after remove: expected 1, got %d", len(perms))
	}
}

func TestMemoryStorage_RemoveRoleForSubjectInDomain(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()

	_ = s.AddRoleForSubjectInDomain(ctx, "user-1", "editor", "org-a")
	_ = s.RemoveRoleForSubjectInDomain(ctx, "user-1", "editor", "org-a")

	roles, _ := s.GetRolesForSubjectInDomain(ctx, "user-1", "org-a")
	if len(roles) != 0 {
		t.Fatalf("expected [], got %v", roles)
	}
}

func TestMemoryStorage_DomainPermissions(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()

	_ = s.AddPermissionInDomain(ctx, "editor", "post", "read", "org-a")
	_ = s.AddPermissionInDomain(ctx, "editor", "post", "read", "org-b")

	permsA, _ := s.GetPermissionsInDomain(ctx, "editor", "org-a")
	if len(permsA) != 1 || !permsA[0].Match("post", "read") {
		t.Fatalf("org-a: expected [post:read], got %v", permsA)
	}

	permsB, _ := s.GetPermissionsInDomain(ctx, "editor", "org-b")
	if len(permsB) != 1 || !permsB[0].Match("post", "read") {
		t.Fatalf("org-b: expected [post:read], got %v", permsB)
	}

	// 全局不应受影响
	permsG, _ := s.GetPermissions(ctx, "editor")
	if len(permsG) != 0 {
		t.Fatalf("global: expected [], got %v", permsG)
	}
}

func TestMemoryStorage_SaveAll_LoadAll(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()

	roleMembers := map[string][]string{
		"user-1": {"admin"},
		"user-2": {"editor:org-a"},
	}
	rolePerms := map[string][]Permission{
		"admin":        {{Resource: "*", Action: "*"}},
		"editor:org-a": {{Resource: "post", Action: "read"}},
	}

	if err := s.SaveAll(ctx, roleMembers, rolePerms); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	loadedMembers, loadedPerms, err := s.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if len(loadedMembers) != 2 {
		t.Fatalf("members: expected 2, got %d", len(loadedMembers))
	}
	if len(loadedPerms) != 2 {
		t.Fatalf("perms: expected 2, got %d", len(loadedPerms))
	}
}
