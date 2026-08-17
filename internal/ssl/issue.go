package ssl

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/services/cloudflare"
	"github.com/lyracorp/xmanager/internal/ssh"
)

// Request describes one hostname to protect (apex, www, or webmail.*).
type Request struct {
	Host        string
	Upstream    string
	Enabled     bool
	Provider    string
	DNSProvider string
	CFZoneID    string
	CFToken     string
	CertZone    string // zone used for Origin CA / custom cert dir
	CustomCert  string
	CustomKey   string
}

// Result is the outcome of Issue.
type Result struct {
	Provider string
	Status   string
	Error    string
}

func (r Result) Err() error {
	if r.Error == "" {
		return nil
	}
	return fmt.Errorf("%s", r.Error)
}

// Issue enables, disables, or provisions TLS for a hostname. Failures are returned in Result, not as a hard error.
func Issue(exec *ssh.Executor, req Request) Result {
	host := strings.ToLower(strings.TrimSpace(req.Host))
	if host == "" {
		return Result{Status: StatusError, Error: "host required"}
	}
	if !req.Enabled {
		return disableHost(exec, host, req.Provider)
	}

	provider := ResolveProvider(req.DNSProvider, req.Provider)
	out := Result{Provider: provider, Status: StatusPending}

	switch provider {
	case ProviderCaddy:
		if err := ApplyCaddy(exec, host, req.Upstream); err != nil {
			out.Status = StatusError
			out.Error = err.Error()
			return out
		}
		out.Status = StatusOK
		return out
	case ProviderCloudflare:
		if err := issueCloudflare(exec, req, host); err != nil {
			out.Status = StatusError
			out.Error = err.Error()
			return out
		}
	case ProviderCustom:
		if err := issueCustom(exec, req, host); err != nil {
			out.Status = StatusError
			out.Error = err.Error()
			return out
		}
	default:
		if err := issueLetsEncrypt(exec, host); err != nil {
			out.Status = StatusError
			out.Error = err.Error()
			return out
		}
	}
	out.Status = StatusOK
	return out
}

func disableHost(exec *ssh.Executor, host, provider string) Result {
	if ResolveProvider("", provider) == ProviderCaddy || provider == ProviderCaddy {
		RemoveCaddySite(exec, host)
	}
	if nm := nginxMgr(exec); nm != nil {
		if err := nm.DisableSSL(host); err != nil {
			// vhost may already be HTTP-only
			_ = err
		}
	}
	return Result{Provider: provider, Status: StatusDisabled}
}

func issueLetsEncrypt(exec *ssh.Executor, host string) error {
	nm := nginxMgr(exec)
	if nm == nil {
		return fmt.Errorf("nginx unavailable")
	}
	if err := nm.EnsureCertbot(); err != nil {
		return err
	}
	cmd := fmt.Sprintf("sudo certbot --nginx -d %s --non-interactive --agree-tos --register-unsafely-without-email --redirect 2>&1", host)
	res, err := exec.Run(cmd)
	out := ""
	if res != nil {
		out = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	}
	if err != nil {
		return fmt.Errorf("certbot: %w (%s)", err, trimOutput(out, 240))
	}
	if res != nil && res.ExitCode != 0 {
		return fmt.Errorf("certbot: %s", trimOutput(out, 240))
	}
	return nil
}

func issueCustom(exec *ssh.Executor, req Request, host string) error {
	zone := strings.TrimSpace(req.CertZone)
	if zone == "" {
		zone = host
	}
	certPath, keyPath := OriginCertPaths(zone)
	if strings.TrimSpace(req.CustomCert) != "" || strings.TrimSpace(req.CustomKey) != "" {
		if !ValidPEM(req.CustomCert, req.CustomKey) {
			return fmt.Errorf("custom SSL: PEM certificate and private key required")
		}
		if err := writeRemoteFile(exec, certPath, strings.TrimSpace(req.CustomCert)+"\n"); err != nil {
			return err
		}
		if err := writeRemoteFile(exec, keyPath, strings.TrimSpace(req.CustomKey)+"\n"); err != nil {
			return err
		}
	} else if exec.RunQuiet("test -f "+certPath+" && test -f "+keyPath+" && echo ok") != "ok" {
		return fmt.Errorf("custom SSL: upload a certificate and key first")
	}
	RemoveCaddySite(exec, host)
	nm := nginxMgr(exec)
	if nm == nil {
		return fmt.Errorf("nginx unavailable")
	}
	return nm.EnableSSL(host, certPath, keyPath)
}

func issueCloudflare(exec *ssh.Executor, req Request, host string) error {
	if strings.TrimSpace(req.CFToken) == "" {
		return fmt.Errorf("cloudflare: API token not set")
	}
	zone := strings.TrimSpace(req.CertZone)
	if zone == "" {
		zone = host
	}
	certPath, keyPath := OriginCertPaths(zone)
	needIssue := exec == nil || exec.RunQuiet("test -f "+certPath+" && test -f "+keyPath+" && echo ok") != "ok"
	if needIssue {
		client := cloudflare.NewClient(cloudflare.NewConfig(req.CFToken, req.CFZoneID))
		hosts := OriginHostnames(zone)
		csr, err := GenerateOriginCSR(hosts)
		if err != nil {
			return err
		}
		certPEM, err := IssueOriginCert(client, csr.CSRPEM, hosts)
		if err != nil {
			return err
		}
		if err := writeRemoteFile(exec, certPath, strings.TrimSpace(certPEM)+"\n"); err != nil {
			return err
		}
		if err := writeRemoteFile(exec, keyPath, strings.TrimSpace(csr.KeyPEM)+"\n"); err != nil {
			return err
		}
		if err := client.SetZoneSSLMode("strict"); err != nil {
			if err2 := client.SetZoneSSLMode("full"); err2 != nil {
				return fmt.Errorf("origin cert saved, but SSL mode: %v; %v", err, err2)
			}
		}
	}
	RemoveCaddySite(exec, host)
	nm := nginxMgr(exec)
	if nm == nil {
		return fmt.Errorf("nginx unavailable")
	}
	return nm.EnableSSL(host, certPath, keyPath)
}
