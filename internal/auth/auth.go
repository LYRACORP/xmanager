package auth

import (
	"errors"
	"fmt"

	"github.com/lyracorp/xmanager/internal/storage"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
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

// EnsureAdmin reports true when no users exist yet (setup required).
func EnsureAdmin(db *gorm.DB) (bool, error) {
	var count int64
	if err := db.Model(&storage.User{}).Count(&count).Error; err != nil {
		return false, fmt.Errorf("counting users: %w", err)
	}
	return count == 0, nil
}

// CreateUser creates a new user with the given role.
func CreateUser(db *gorm.DB, username, password, role string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	u := storage.User{
		Username:     username,
		PasswordHash: hash,
		Role:         role,
	}
	if err := db.Create(&u).Error; err != nil {
		return fmt.Errorf("creating user: %w", err)
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
	if !CheckPassword(u.PasswordHash, password) {
		return nil, errors.New("invalid credentials")
	}
	return &u, nil
}
