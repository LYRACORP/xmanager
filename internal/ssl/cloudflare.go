package ssl

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/services/cloudflare"
)

// OriginCSR holds a generated private key and CSR PEM for Origin CA.
type OriginCSR struct {
	KeyPEM string
	CSRPEM string
}

// GenerateOriginCSR creates an RSA key + CSR covering the given hostnames.
func GenerateOriginCSR(hostnames []string) (*OriginCSR, error) {
	if len(hostnames) == 0 {
		return nil, fmt.Errorf("hostnames required")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	cn := hostnames[0]
	tpl := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: cn},
		DNSNames: hostnames,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tpl, key)
	if err != nil {
		return nil, err
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return &OriginCSR{KeyPEM: string(keyPEM), CSRPEM: string(csrPEM)}, nil
}

// OriginCAPayload is the JSON body for POST /certificates.
func OriginCAPayload(csrPEM string, hostnames []string) map[string]any {
	return map[string]any{
		"csr":                strings.TrimSpace(csrPEM),
		"hostnames":          hostnames,
		"request_type":       "origin-rsa",
		"requested_validity": 5475,
	}
}

type originCertResult struct {
	Certificate string `json:"certificate"`
}

// IssueOriginCert requests a Cloudflare Origin CA certificate and returns the cert PEM.
func IssueOriginCert(client *cloudflare.Client, csrPEM string, hostnames []string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("cloudflare client required")
	}
	raw, err := client.CreateOriginCertificate(OriginCAPayload(csrPEM, hostnames))
	if err != nil {
		return "", err
	}
	var out originCertResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode origin cert: %w", err)
	}
	if strings.TrimSpace(out.Certificate) == "" {
		return "", fmt.Errorf("cloudflare origin CA returned empty certificate")
	}
	return out.Certificate, nil
}
