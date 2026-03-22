package bot

import (
	"testing"

	"github.com/vinegod/discordgamebridge/internal/config"
)

// --- User allowlist ---

func TestCheckPermission_AllowedUser_Grants(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedUsers: []string{"user-123"},
	}
	if !checkPermission("user-123", nil, perms) {
		t.Error("listed user should be allowed")
	}
}

func TestCheckPermission_UnlistedUser_Denies(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedUsers: []string{"user-123"},
	}
	if checkPermission("user-999", nil, perms) {
		t.Error("unlisted user should be denied")
	}
}

func TestCheckPermission_MultipleAllowedUsers_CorrectOneGrants(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedUsers: []string{"alice", "bob", "carol"},
	}
	if !checkPermission("bob", nil, perms) {
		t.Error("bob is listed and should be allowed")
	}
	if checkPermission("mallory", nil, perms) {
		t.Error("mallory is not listed and should be denied")
	}
}

// --- Role allowlist ---

func TestCheckPermission_AllowedRole_Grants(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedRoles: []string{"admin-role"},
	}
	if !checkPermission("user-x", []string{"admin-role", "other-role"}, perms) {
		t.Error("user with allowed role should be granted")
	}
}

func TestCheckPermission_NoMatchingRole_Denies(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedRoles: []string{"admin-role"},
	}
	if checkPermission("user-x", []string{"member-role"}, perms) {
		t.Error("user without allowed role should be denied")
	}
}

func TestCheckPermission_NoRoles_Denies(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedRoles: []string{"admin-role"},
	}
	if checkPermission("user-x", nil, perms) {
		t.Error("user with no roles should be denied when roles are required")
	}
}

func TestCheckPermission_EveryoneRole_AllowsAll(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedRoles: []string{"@everyone"},
	}
	if !checkPermission("any-user", nil, perms) {
		t.Error("@everyone role should allow any user regardless of their roles")
	}
}

// --- Combined user + role lists ---

func TestCheckPermission_UserAllowed_EvenWithoutMatchingRole(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedUsers: []string{"superadmin"},
		AllowedRoles: []string{"admin-role"},
	}
	// superadmin has no roles but is in the user list — should still be granted.
	if !checkPermission("superadmin", nil, perms) {
		t.Error("user in AllowedUsers should be granted even without matching roles")
	}
}

func TestCheckPermission_RoleAllowed_UserNotInUserList(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedUsers: []string{"specific-user"},
		AllowedRoles: []string{"mod-role"},
	}
	if !checkPermission("different-user", []string{"mod-role"}, perms) {
		t.Error("user with allowed role should be granted even if not in AllowedUsers")
	}
}

func TestCheckPermission_NeitherUserNorRole_Denies(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedUsers: []string{"alice"},
		AllowedRoles: []string{"admin-role"},
	}
	if checkPermission("mallory", []string{"member-role"}, perms) {
		t.Error("user with no match in users or roles should be denied")
	}
}

// --- DM context (no guild member, so no roles) ---

func TestCheckPermission_NoMember_RoleRequired_Denies(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedRoles: []string{"admin-role"},
	}
	// nil roleIDs simulates a DM where there is no guild member.
	if checkPermission("user-x", nil, perms) {
		t.Error("user in DM (no roles) should be denied for a role-required command")
	}
}

func TestCheckPermission_NoMember_UserAllowed_Grants(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedUsers: []string{"user-123"},
	}
	if !checkPermission("user-123", nil, perms) {
		t.Error("user in DM should be granted if they are in AllowedUsers")
	}
}
