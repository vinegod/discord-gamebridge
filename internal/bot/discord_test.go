package bot

import (
	"testing"

	"github.com/vinegod/discordgamebridge/internal/config"
)

// ── checkPermission ───────────────────────────────────────────────────────────

func TestCheckPermission_AllowedUser_Grants(t *testing.T) {
	perms := config.PermissionConfig{AllowedUsers: []string{"user-123"}}
	if !checkPermission("user-123", nil, perms) {
		t.Error("listed user should be allowed")
	}
}

func TestCheckPermission_UnlistedUser_Denies(t *testing.T) {
	perms := config.PermissionConfig{AllowedUsers: []string{"user-123"}}
	if checkPermission("user-999", nil, perms) {
		t.Error("unlisted user should be denied")
	}
}

func TestCheckPermission_MultipleAllowedUsers_CorrectOneGrants(t *testing.T) {
	perms := config.PermissionConfig{AllowedUsers: []string{"alice", "bob", "carol"}}
	if !checkPermission("bob", nil, perms) {
		t.Error("bob is listed and should be allowed")
	}
	if checkPermission("mallory", nil, perms) {
		t.Error("mallory is not listed and should be denied")
	}
}

func TestCheckPermission_AllowedRole_Grants(t *testing.T) {
	perms := config.PermissionConfig{AllowedRoles: []string{"admin-role"}}
	if !checkPermission("user-x", []string{"admin-role", "other-role"}, perms) {
		t.Error("user with allowed role should be granted")
	}
}

func TestCheckPermission_NoMatchingRole_Denies(t *testing.T) {
	perms := config.PermissionConfig{AllowedRoles: []string{"admin-role"}}
	if checkPermission("user-x", []string{"member-role"}, perms) {
		t.Error("user without allowed role should be denied")
	}
}

func TestCheckPermission_NoRoles_Denies(t *testing.T) {
	perms := config.PermissionConfig{AllowedRoles: []string{"admin-role"}}
	if checkPermission("user-x", nil, perms) {
		t.Error("user with no roles should be denied when roles are required")
	}
}

func TestCheckPermission_EveryoneRole_AllowsAll(t *testing.T) {
	perms := config.PermissionConfig{AllowedRoles: []string{"@everyone"}}
	if !checkPermission("any-user", nil, perms) {
		t.Error("@everyone role should allow any user regardless of their roles")
	}
}

func TestCheckPermission_UserAllowed_EvenWithoutMatchingRole(t *testing.T) {
	perms := config.PermissionConfig{
		AllowedUsers: []string{"superadmin"},
		AllowedRoles: []string{"admin-role"},
	}
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

func TestCheckPermission_NoMember_RoleRequired_Denies(t *testing.T) {
	perms := config.PermissionConfig{AllowedRoles: []string{"admin-role"}}
	if checkPermission("user-x", nil, perms) {
		t.Error("user in DM (no roles) should be denied for a role-required command")
	}
}

func TestCheckPermission_NoMember_UserAllowed_Grants(t *testing.T) {
	perms := config.PermissionConfig{AllowedUsers: []string{"user-123"}}
	if !checkPermission("user-123", nil, perms) {
		t.Error("user in DM should be granted if they are in AllowedUsers")
	}
}

// ── substituteTemplate ────────────────────────────────────────────────────────

func SubstituteTemplate_AllPlaceholdersFilled(t *testing.T) {
	result := config.SubstituteTemplate(
		"kick {{.player}} {{.reason}}",
		map[string]string{"player": "Alice", "reason": "griefing"},
	)
	if result != "kick Alice griefing" {
		t.Errorf("expected 'kick Alice griefing', got %q", result)
	}
}

func SubstituteTemplate_SinglePlaceholder(t *testing.T) {
	result := config.SubstituteTemplate("kick {{.player}}", map[string]string{"player": "Bob"})
	if result != "kick Bob" {
		t.Errorf("expected 'kick Bob', got %q", result)
	}
}

func SubstituteTemplate_NoPlaceholders(t *testing.T) {
	result := config.SubstituteTemplate("save", map[string]string{})
	if result != "save" {
		t.Errorf("expected 'save', got %q", result)
	}
}

func SubstituteTemplate_NilValues_RemovesAllPlaceholders(t *testing.T) {
	result := config.SubstituteTemplate("kick {{.player}}", nil)
	if result != "kick" {
		t.Errorf("expected empty string with nil values, got %q", result)
	}
}

func SubstituteTemplate_OptionalMissing_RemovedCleanly(t *testing.T) {
	// reason is optional and not provided — placeholder must be removed,
	// leaving no double space or trailing garbage.
	result := config.SubstituteTemplate(
		"kick {{.player}} {{.reason}}",
		map[string]string{"player": "Alice"}, // reason omitted
	)
	if result != "kick Alice" {
		t.Errorf("expected 'kick Alice' with missing optional arg removed, got %q", result)
	}
}

func SubstituteTemplate_AllPlaceholdersMissing_ReturnsEmpty(t *testing.T) {
	result := config.SubstituteTemplate("{{.player}}", map[string]string{})
	if result != "" {
		t.Errorf("expected empty string when all placeholders missing, got %q", result)
	}
}

func SubstituteTemplate_EmptyTemplate_ReturnsEmpty(t *testing.T) {
	result := config.SubstituteTemplate("", map[string]string{"player": "Alice"})
	if result != "" {
		t.Errorf("expected empty string for empty template, got %q", result)
	}
}

func SubstituteTemplate_ExtraValuesIgnored(t *testing.T) {
	// Values that don't correspond to any placeholder are silently ignored.
	result := config.SubstituteTemplate("save", map[string]string{"unused": "value"})
	if result != "save" {
		t.Errorf("expected 'save' with extra values ignored, got %q", result)
	}
}

func SubstituteTemplate_ValueWithSpecialChars_PassedThrough(t *testing.T) {
	// Player names can contain spaces, punctuation, etc.
	// The function must not sanitize values — that's the caller's job.
	result := config.SubstituteTemplate("kick {{.player}}", map[string]string{"player": "Player One"})
	if result != "kick Player One" {
		t.Errorf("expected 'kick Player One', got %q", result)
	}
}

func SubstituteTemplate_TrimmedResult(t *testing.T) {
	// When the only content is a removed placeholder, TrimSpace must clean up.
	result := config.SubstituteTemplate("  {{.player}}  ", map[string]string{})
	if result != "" {
		t.Errorf("expected empty trimmed result, got %q", result)
	}
}
