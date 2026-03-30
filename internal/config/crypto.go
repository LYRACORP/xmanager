package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

func machineID() string {
	var raw string
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/etc/machine-id")
		if err == nil {
			raw = strings.TrimSpace(string(data))
		}
	case "darwin":
		data, err := os.ReadFile("/Library/Preferences/SystemConfiguration/com.apple.smb.server.plist")
		if err == nil {
			raw = string(data)
		}
	case "windows":
		// This is a simple fallback for Windows without adding heavy registry dependencies
		// In a real app, you'd use golang.org/x/sys/windows/registry
		raw = os.Getenv("COMPUTERNAME") + os.Getenv("PROCESSOR_IDENTIFIER")
	}
	if raw == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			raw = "xmanager-fallback-global"
		} else {
			raw = "xmanager-fallback-" + home
		}
	}
	return raw
}

func deriveKey() []byte {
	h := sha256.Sum256([]byte(machineID()))
	return h[:]
}

func Encrypt(plaintext string) (string, error) {
	key := deriveKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func Decrypt(encoded string) (string, error) {
	key := deriveKey()
	data, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decoding hex: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}

	return string(plaintext), nil
}
