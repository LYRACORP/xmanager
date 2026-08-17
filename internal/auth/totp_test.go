package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestGenerateAndValidateTOTP(t *testing.T) {
	en, err := GenerateSecret("alice", "XManager")
	if err != nil {
		t.Fatal(err)
	}
	if en.Secret == "" || en.URL == "" || en.QRDataURI == "" {
		t.Fatalf("empty enrollment: %+v", en)
	}
	if !strings.Contains(en.QRDataURI, "data:image/png;base64,") {
		t.Fatalf("qr uri %q", en.QRDataURI)
	}
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	code, err := totp.GenerateCodeCustom(en.Secret, now, totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidateCodeAt(en.Secret, code, now) {
		t.Fatal("expected valid code")
	}
	if ValidateCodeAt(en.Secret, "000000", now) {
		t.Fatal("expected reject")
	}
}

func TestEncryptSecretRoundTrip(t *testing.T) {
	enc, err := EncryptSecret("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "" || enc == "JBSWY3DPEHPK3PXP" {
		t.Fatal("expected ciphertext")
	}
	got, err := DecryptSecret(enc)
	if err != nil || got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestRecoveryCodesConsumeOnce(t *testing.T) {
	plain, hashed, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 8 || RecoveryRemaining(hashed) != 8 {
		t.Fatalf("plain=%d remaining=%d", len(plain), RecoveryRemaining(hashed))
	}
	next, ok := ConsumeRecoveryCode(hashed, plain[0])
	if !ok {
		t.Fatal("expected consume")
	}
	if RecoveryRemaining(next) != 7 {
		t.Fatalf("remaining %d", RecoveryRemaining(next))
	}
	if _, ok := ConsumeRecoveryCode(next, plain[0]); ok {
		t.Fatal("code should be one-time")
	}
	if _, ok := ConsumeRecoveryCode(hashed, "not-a-code"); ok {
		t.Fatal("garbage")
	}
}
