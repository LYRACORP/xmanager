package auth

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&storage.User{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestIsAdmin(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{"admin", true},
		{"Admin", true},
		{"user", false},
		{"operator", false},
		{"viewer", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := IsAdmin(tc.role); got != tc.want {
			t.Fatalf("IsAdmin(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
}

func TestNormalizeRole(t *testing.T) {
	if NormalizeRole("operator") != RoleUser {
		t.Fatalf("operator should map to user")
	}
	if NormalizeRole("admin") != RoleAdmin {
		t.Fatalf("admin should stay admin")
	}
}

func TestAuthenticateRejectsDisabled(t *testing.T) {
	db := testDB(t)
	if err := CreateUser(db, "bob", "secret", RoleUser); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&storage.User{}).Where("username = ?", "bob").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Authenticate(db, "bob", "secret"); err == nil {
		t.Fatal("expected disabled user to be rejected")
	}
}

func TestCreateUserDefaultRole(t *testing.T) {
	db := testDB(t)
	if err := CreateUser(db, "alice", "pass", ""); err != nil {
		t.Fatal(err)
	}
	var u storage.User
	if err := db.Where("username = ?", "alice").First(&u).Error; err != nil {
		t.Fatal(err)
	}
	if u.Role != RoleUser {
		t.Fatalf("role = %q, want user", u.Role)
	}
	if !u.Enabled {
		t.Fatal("expected enabled by default")
	}
}

func TestSetPassword(t *testing.T) {
	db := testDB(t)
	if err := CreateUser(db, "carol", "old", RoleUser); err != nil {
		t.Fatal(err)
	}
	var u storage.User
	db.Where("username = ?", "carol").First(&u)
	if err := SetPassword(db, u.ID, "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := Authenticate(db, "carol", "old"); err == nil {
		t.Fatal("old password should fail")
	}
	if _, err := Authenticate(db, "carol", "new"); err != nil {
		t.Fatal("new password should work")
	}
}

func TestCountEnabledAdmins(t *testing.T) {
	db := testDB(t)
	_ = CreateUser(db, "a1", "p", RoleAdmin)
	_ = CreateUser(db, "u1", "p", RoleUser)
	n, err := CountEnabledAdmins(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}
