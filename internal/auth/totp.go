package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	totpIssuer       = "XManager"
	recoveryCount    = 8
	recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

// Enrollment is a pending TOTP setup (secret not yet stored).
type Enrollment struct {
	Secret    string
	URL       string
	QRDataURI string
}

// GenerateSecret creates a new TOTP secret and otpauth URL + QR data URI.
func GenerateSecret(username, issuer string) (Enrollment, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return Enrollment{}, fmt.Errorf("username required")
	}
	if strings.TrimSpace(issuer) == "" {
		issuer = totpIssuer
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: username,
	})
	if err != nil {
		return Enrollment{}, fmt.Errorf("generating totp secret: %w", err)
	}
	img, err := key.Image(200, 200)
	if err != nil {
		return Enrollment{}, fmt.Errorf("encoding qr: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return Enrollment{}, fmt.Errorf("encoding qr png: %w", err)
	}
	return Enrollment{
		Secret:    key.Secret(),
		URL:       key.URL(),
		QRDataURI: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
	}, nil
}

func totpOpts() totp.ValidateOpts {
	return totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
}

// ValidateCode reports whether code is a valid TOTP for secret (current time).
func ValidateCode(secret, code string) bool {
	return ValidateCodeAt(secret, code, time.Now())
}

// ValidateCodeAt validates a TOTP at a specific time (tests).
func ValidateCodeAt(secret, code string, t time.Time) bool {
	secret = strings.TrimSpace(secret)
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if secret == "" || code == "" {
		return false
	}
	ok, err := totp.ValidateCustom(code, secret, t, totpOpts())
	return err == nil && ok
}

// EncryptSecret encrypts a TOTP secret for storage.
func EncryptSecret(plain string) (string, error) {
	return config.Encrypt(plain)
}

// DecryptSecret decrypts a stored TOTP secret.
func DecryptSecret(enc string) (string, error) {
	return config.Decrypt(enc)
}

// GenerateRecoveryCodes returns plaintext codes and a JSON array of bcrypt hashes.
func GenerateRecoveryCodes() (plain []string, hashedJSON string, err error) {
	plain = make([]string, recoveryCount)
	hashes := make([]string, recoveryCount)
	for i := 0; i < recoveryCount; i++ {
		c, err := randomRecoveryCode()
		if err != nil {
			return nil, "", err
		}
		plain[i] = c
		h, err := HashPassword(normalizeRecovery(c))
		if err != nil {
			return nil, "", err
		}
		hashes[i] = h
	}
	b, err := json.Marshal(hashes)
	if err != nil {
		return nil, "", err
	}
	return plain, string(b), nil
}

// ConsumeRecoveryCode removes a matching hashed code. ok is false if unused.
func ConsumeRecoveryCode(hashedJSON, code string) (string, bool) {
	code = normalizeRecovery(code)
	if code == "" || strings.TrimSpace(hashedJSON) == "" {
		return hashedJSON, false
	}
	var hashes []string
	if err := json.Unmarshal([]byte(hashedJSON), &hashes); err != nil {
		return hashedJSON, false
	}
	for i, h := range hashes {
		if CheckPassword(h, code) {
			hashes = append(hashes[:i], hashes[i+1:]...)
			b, err := json.Marshal(hashes)
			if err != nil {
				return hashedJSON, false
			}
			return string(b), true
		}
	}
	return hashedJSON, false
}

// RecoveryRemaining counts unused recovery hashes.
func RecoveryRemaining(hashedJSON string) int {
	if strings.TrimSpace(hashedJSON) == "" {
		return 0
	}
	var hashes []string
	if err := json.Unmarshal([]byte(hashedJSON), &hashes); err != nil {
		return 0
	}
	return len(hashes)
}

func normalizeRecovery(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, " ", "")
	code = strings.ReplaceAll(code, "-", "")
	return code
}

func randomRecoveryCode() (string, error) {
	raw := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	var b strings.Builder
	alpha := recoveryAlphabet
	for i, n := range raw {
		if i == 4 {
			b.WriteByte('-')
		}
		b.WriteByte(alpha[int(n)%len(alpha)])
	}
	return b.String(), nil
}
