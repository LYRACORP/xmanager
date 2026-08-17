package auth

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/storage"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// HashPassword returns a bcrypt hash of the given password.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(b), nil
}

// CheckPassword reports whether password matches hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// IsAdmin reports whether role grants full panel access.
func IsAdmin(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), RoleAdmin)
}

// NormalizeRole maps legacy roles to admin or user.
func NormalizeRole(role string) string {
	if IsAdmin(role) {
		return RoleAdmin
	}
	return RoleUser
}

// EnsureAdmin reports true when no users exist yet (setup required).
func EnsureAdmin(db *gorm.DB) (bool, error) {
	var count int64
	if err := db.Model(&storage.User{}).Count(&count).Error; err != nil {
		return false, fmt.Errorf("counting users: %w", err)
	}
	return count == 0, nil
}

// CountEnabledAdmins returns the number of enabled admin accounts.
func CountEnabledAdmins(db *gorm.DB) (int64, error) {
	var count int64
	err := db.Model(&storage.User{}).Where("role = ? AND enabled = ?", RoleAdmin, true).Count(&count).Error
	return count, err
}

// CreateUser creates a new user with the given role (defaults to user).
func CreateUser(db *gorm.DB, username, password, role string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	if strings.TrimSpace(role) == "" {
		role = RoleUser
	}
	u := storage.User{
		Username:     username,
		PasswordHash: hash,
		Role:         NormalizeRole(role),
		Enabled:      true,
	}
	if err := db.Create(&u).Error; err != nil {
		return fmt.Errorf("creating user: %w", err)
	}
	return nil
}

// SetPassword updates a user's password hash.
func SetPassword(db *gorm.DB, userID uint, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	res := db.Model(&storage.User{}).Where("id = ?", userID).Update("password_hash", hash)
	if res.Error != nil {
		return fmt.Errorf("updating password: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Authenticate validates credentials and returns the matching user.
func Authenticate(db *gorm.DB, username, password string) (*storage.User, error) {
	var u storage.User
	if err := db.Where("username = ?", username).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid credentials")
		}
		return nil, fmt.Errorf("looking up user: %w", err)
	}
	if !u.Enabled {
		return nil, errors.New("account disabled")
	}
	if !CheckPassword(u.PasswordHash, password) {
		return nil, errors.New("invalid credentials")
	}
	return &u, nil
}
