package project

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthenticatedRepoURL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&storage.GitCredential{})
	enc, err := config.Encrypt("tok123")
	if err != nil {
		t.Fatal(err)
	}
	cred := storage.GitCredential{Provider: "github", TokenEncrypted: enc}
	if err := db.Create(&cred).Error; err != nil {
		t.Fatal(err)
	}
	d := NewDeployer(nil, db)
	got, err := d.authenticatedRepoURL(&Config{CredID: cred.ID}, "https://github.com/acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(got, "tok123") || !containsStr(got, "x-access-token") {
		t.Fatalf("got %q", got)
	}
	plain, err := d.authenticatedRepoURL(&Config{}, "https://github.com/acme/app.git")
	if err != nil || plain != "https://github.com/acme/app.git" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
