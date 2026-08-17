package waf

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/proxy"
	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/ssh"
)

const modsecSnippetPath = "/etc/nginx/snippets/xmanager-modsec.conf"
const modsecDir = "/etc/nginx/modsecurity"

// ModsecStatus reports whether ModSecurity nginx module is present.
type ModsecStatus struct {
	Installed bool
	Enabled   bool
	Detail    string
}

// DetectModsec checks for modsecurity nginx module.
func DetectModsec(exec *ssh.Executor) ModsecStatus {
	if exec == nil {
		return ModsecStatus{}
	}
	out := exec.RunQuiet("dpkg -l 2>/dev/null | grep -i modsecurity; nginx -V 2>&1 | grep -i modsecurity")
	installed := strings.Contains(strings.ToLower(out), "modsecurity")
	enabled := strings.TrimSpace(exec.RunQuiet(fmt.Sprintf("test -f %s/modsecurity.conf && echo yes", modsecDir))) == "yes"
	return ModsecStatus{Installed: installed, Enabled: enabled, Detail: strings.TrimSpace(out)}
}

// InstallModsec installs modsecurity module and OWASP CRS (best effort on Ubuntu).
func InstallModsec(exec *ssh.Executor, p security.Policy) error {
	if exec == nil {
		return fmt.Errorf("no executor")
	}
	cmds := []string{
		"sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq",
		"sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq libnginx-mod-http-modsecurity 2>/dev/null || sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nginx-module-security 2>/dev/null || true",
		fmt.Sprintf("sudo mkdir -p %s/crs", modsecDir),
		fmt.Sprintf("if [ ! -f %s/crs/crs-setup.conf ]; then sudo git clone --depth 1 --branch v4.4.0 https://github.com/coreruleset/coreruleset.git %s/crs-tmp 2>/dev/null && sudo mv %s/crs-tmp/* %s/crs/ && sudo rmdir %s/crs-tmp 2>/dev/null || true; fi", modsecDir, modsecDir, modsecDir, modsecDir, modsecDir),
	}
	for _, c := range cmds {
		_ = exec.RunQuiet(c)
	}
	mode := "On"
	if p.ModsecDetectOnly {
		mode = "DetectionOnly"
	}
	paranoia := p.ModsecParanoia
	if paranoia < 1 {
		paranoia = 1
	}
	if paranoia > 2 {
		paranoia = 2
	}
	mainConf := fmt.Sprintf(`# xmanager modsecurity
SecRuleEngine %s
Include %s/crs/crs-setup.conf
Include %s/crs/rules/*.conf
`, mode, modsecDir, modsecDir)
	_ = exec.RunQuiet(fmt.Sprintf("sudo mkdir -p %s && sudo tee %s/modsecurity.conf > /dev/null << 'XMEOF'\n%s\nXMEOF", modsecDir, modsecDir, mainConf))

	snippet := fmt.Sprintf(`# xmanager-modsec
modsecurity on;
modsecurity_rules_file %s/modsecurity.conf;
`, modsecDir)
	write := fmt.Sprintf("sudo mkdir -p /etc/nginx/snippets && sudo tee %s > /dev/null << 'XMEOF'\n%s\nXMEOF", modsecSnippetPath, snippet)
	_ = exec.RunQuiet(write)
	return patchVhostsInclude(exec, modsecSnippetPath, "xmanager-modsec")
}

// RemoveModsec removes modsecurity includes from vhosts.
func RemoveModsec(exec *ssh.Executor) error {
	if exec == nil {
		return nil
	}
	mgr := proxy.NewManager(proxy.Nginx, exec)
	if mgr != nil {
		vhosts, _ := mgr.ListVHosts()
		for _, v := range vhosts {
			if v.ConfigFile != "" {
				_ = exec.RunQuiet(fmt.Sprintf("sudo sed -i '/xmanager-modsec/d' %s", shellSafe(v.ConfigFile)))
			}
		}
		_ = mgr.ReloadConfig()
	}
	return nil
}

// ApplyModsec enables or disables modsecurity per policy.
func ApplyModsec(exec *ssh.Executor, p security.Policy) error {
	if !p.Enabled || !p.WAFModsec {
		return RemoveModsec(exec)
	}
	st := DetectModsec(exec)
	if !st.Installed {
		if err := InstallModsec(exec, p); err != nil {
			return err
		}
	}
	return patchVhostsInclude(exec, modsecSnippetPath, "xmanager-modsec")
}

func patchVhostsInclude(exec *ssh.Executor, snippetPath, marker string) error {
	mgr := proxy.NewManager(proxy.Nginx, exec)
	if mgr == nil {
		return fmt.Errorf("nginx unavailable")
	}
	vhosts, err := mgr.ListVHosts()
	if err != nil {
		return err
	}
	for _, v := range vhosts {
		if v.ConfigFile == "" {
			continue
		}
		patch := fmt.Sprintf(`if ! grep -q '%s' %s 2>/dev/null; then sudo sed -i '/server {/a\\    include %s;' %s; fi`,
			marker, shellSafe(v.ConfigFile), snippetPath, shellSafe(v.ConfigFile))
		_ = exec.RunQuiet(patch)
	}
	return mgr.ReloadConfig()
}

func shellSafe(s string) string {
	return strings.NewReplacer("'", "", ";", "", "&", "", "|", "", "`", "").Replace(s)
}
