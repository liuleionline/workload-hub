package main

import "testing"

func TestRoleDisplayNames(t *testing.T) {
	tests := map[string]string{
		"manager":  "部门领导",
		"lead":     "专业负责人",
		"designer": "设计师",
	}
	for role, want := range tests {
		if got := (User{Role: role}).RoleName(); got != want {
			t.Fatalf("role %s display name=%q, want %q", role, got, want)
		}
	}
}

func TestNewAndLegacyImportedRoleNames(t *testing.T) {
	tests := map[string]string{
		"部门领导":   "manager",
		"管理者":    "manager",
		"专业负责人":  "lead",
		"设计师":    "designer",
		"一般设计人员": "designer",
	}
	for label, want := range tests {
		got, err := importedRole(label)
		if err != nil {
			t.Fatalf("import role %q: %v", label, err)
		}
		if got != want {
			t.Fatalf("import role %q=%q, want %q", label, got, want)
		}
	}
}
