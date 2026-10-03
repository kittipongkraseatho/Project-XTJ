package rbac

import "testing"

func TestRoleRank_Ascending(t *testing.T) {
	if RoleRank(RoleViewer) >= RoleRank(RoleMember) {
		t.Error("viewer should rank below member")
	}
	if RoleRank(RoleMember) >= RoleRank(RoleAdmin) {
		t.Error("member should rank below admin")
	}
	if RoleRank(RoleAdmin) >= RoleRank(RoleOwner) {
		t.Error("admin should rank below owner")
	}
	if RoleRank("unknown") != 0 {
		t.Error("unknown role should be 0")
	}
}

func TestHasRole(t *testing.T) {
	if !HasRole(RoleOwner, RoleMember) {
		t.Error("owner satisfies member requirement")
	}
	if !HasRole(RoleAdmin, RoleAdmin) {
		t.Error("admin satisfies admin requirement")
	}
	if HasRole(RoleMember, RoleAdmin) {
		t.Error("member should NOT satisfy admin requirement")
	}
}