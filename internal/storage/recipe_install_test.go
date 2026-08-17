package storage

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openRecipeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Server{}, &RecipeInstall{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRecipeInstallUpsertAndDelete(t *testing.T) {
	db := openRecipeDB(t)
	srv := Server{Name: "box", Host: "192.0.2.1", User: "root"}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	if err := UpsertRecipeInstall(db, srv.ID, "docker"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertRecipeInstall(db, srv.ID, "docker"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertRecipeInstall(db, srv.ID, "linux-harden"); err != nil {
		t.Fatal(err)
	}
	rows, err := ListRecipeInstalls(db, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if err := DeleteRecipeInstall(db, srv.ID, "docker"); err != nil {
		t.Fatal(err)
	}
	rows, err = ListRecipeInstalls(db, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RecipeID != "linux-harden" {
		t.Fatalf("after delete: %+v", rows)
	}
}
