package templates

import (
	"net/http/httptest"
	"testing"
)

func TestBuildPageData_Public(t *testing.T) {
	r := httptest.NewRequest("GET", "/public", nil)
	pd := BuildPageData(r, true)

	if pd.IsPublic != true {
		t.Error("IsPublic should be true")
	}
	if pd.StatusIcon != "\U0001F310" {
		t.Errorf("StatusIcon = %q, want globe", pd.StatusIcon)
	}
	if pd.Decision != "N/A" {
		t.Errorf("Decision = %q, want N/A", pd.Decision)
	}
	if pd.Path != "/public" {
		t.Errorf("Path = %q, want /public", pd.Path)
	}
}

func TestBuildPageData_Authenticated(t *testing.T) {
	r := httptest.NewRequest("GET", "/private", nil)
	r.Header.Set("x-current-user", "alice")
	r.Header.Set("x-user-role", "admin, editor")
	r.Header.Set("x-user-metadata", "allowed")

	pd := BuildPageData(r, false)

	if pd.IsPublic != false {
		t.Error("IsPublic should be false")
	}
	if pd.StatusIcon != "\u2705" {
		t.Errorf("StatusIcon = %q, want checkmark", pd.StatusIcon)
	}
	if pd.Username != "alice" {
		t.Errorf("Username = %q, want alice", pd.Username)
	}
	if pd.Roles != "admin, editor" {
		t.Errorf("Roles = %q, want 'admin, editor'", pd.Roles)
	}
	if pd.Decision != "allowed" {
		t.Errorf("Decision = %q, want allowed", pd.Decision)
	}
}

func TestBuildPageData_RoleParsing(t *testing.T) {
	tests := []struct {
		name     string
		roles    string
		wantLen  int
		wantList []string
	}{
		{"single role", "admin", 1, []string{"admin"}},
		{"multiple roles", "admin,editor,viewer", 3, []string{"admin", "editor", "viewer"}},
		{"with whitespace", " admin , editor ", 2, []string{"admin", "editor"}},
		{"empty string", "", 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("x-user-role", tt.roles)
			pd := BuildPageData(r, false)

			if len(pd.RoleList) != tt.wantLen {
				t.Errorf("RoleList len = %d, want %d", len(pd.RoleList), tt.wantLen)
			}
			for i, want := range tt.wantList {
				if i < len(pd.RoleList) && pd.RoleList[i] != want {
					t.Errorf("RoleList[%d] = %q, want %q", i, pd.RoleList[i], want)
				}
			}
		})
	}
}

func TestInit(t *testing.T) {
	// Uses actual template files in same directory
	Init(".")
	if Page == nil {
		t.Error("Page template is nil after Init")
	}
	if Dossiers == nil {
		t.Error("Dossiers template is nil after Init")
	}
}
