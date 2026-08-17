package storage

import "gorm.io/gorm"

// UpsertRecipeInstall records that a TUI recipe was installed on a server.
func UpsertRecipeInstall(db *gorm.DB, serverID uint, recipeID string) error {
	if db == nil || serverID == 0 || recipeID == "" {
		return nil
	}
	var row RecipeInstall
	err := db.Where("server_id = ? AND recipe_id = ?", serverID, recipeID).First(&row).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return db.Create(&RecipeInstall{ServerID: serverID, RecipeID: recipeID}).Error
}

// DeleteRecipeInstall removes the install record after a successful uninstall.
func DeleteRecipeInstall(db *gorm.DB, serverID uint, recipeID string) error {
	if db == nil || serverID == 0 || recipeID == "" {
		return nil
	}
	return db.Unscoped().Where("server_id = ? AND recipe_id = ?", serverID, recipeID).Delete(&RecipeInstall{}).Error
}

// ListRecipeInstalls returns TUI recipe installs for a server, newest first.
func ListRecipeInstalls(db *gorm.DB, serverID uint) ([]RecipeInstall, error) {
	if db == nil || serverID == 0 {
		return nil, nil
	}
	var rows []RecipeInstall
	err := db.Where("server_id = ?", serverID).Order("created_at DESC").Find(&rows).Error
	return rows, err
}
