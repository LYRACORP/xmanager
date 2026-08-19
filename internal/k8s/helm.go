package k8s

import (
	"context"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

func (e *Engine) Helm(ctx context.Context, clusterID uint, args ...string) (string, error) {
	exec, err := e.ControlPlaneExecutor(clusterID)
	if err != nil {
		return "", err
	}
	if err := e.ensureHelm(exec); err != nil {
		return "", err
	}
	cmd := "helm " + shellJoin(args)
	res, err := exec.Run(cmd)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	if res.ExitCode != 0 {
		return out, fmt.Errorf("helm exit %d: %s", res.ExitCode, truncate(out, 800))
	}
	return strings.TrimSpace(res.Stdout), nil
}

func (e *Engine) ensureHelm(exec *ssh.Executor) error {
	if exec.RunQuiet("command -v helm >/dev/null && echo yes") == "yes" {
		return nil
	}
	res, err := exec.Run("curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash")
	if err != nil {
		return fmt.Errorf("installing helm: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("installing helm: %s", res.Stderr+res.Stdout)
	}
	return nil
}

func (e *Engine) HelmList(ctx context.Context, clusterID uint, ns string) (string, error) {
	args := []string{"list", "-o", "json"}
	if ns == "" || ns == "-A" || ns == "all" {
		args = append(args, "-A")
	} else {
		args = append(args, "-n", ns)
	}
	return e.Helm(ctx, clusterID, args...)
}

func (e *Engine) HelmInstall(ctx context.Context, clusterID uint, name, chart, ns, values string) (string, error) {
	if name == "" || chart == "" {
		return "", fmt.Errorf("release name and chart are required")
	}
	args := []string{"upgrade", "--install", name, chart}
	if ns != "" {
		args = append(args, "-n", ns, "--create-namespace")
	}
	if values != "" {
		args = append(args, "-f", "-")
		exec, err := e.ControlPlaneExecutor(clusterID)
		if err != nil {
			return "", err
		}
		if err := e.ensureHelm(exec); err != nil {
			return "", err
		}
		res, err := exec.Run("cat <<'XM_HELM_EOF' | helm " + shellJoin(args) + "\n" + values + "\nXM_HELM_EOF")
		if err != nil {
			return "", err
		}
		if res.ExitCode != 0 {
			return "", fmt.Errorf("helm: %s", res.Stderr+res.Stdout)
		}
		return res.Stdout, nil
	}
	return e.Helm(ctx, clusterID, args...)
}

func (e *Engine) HelmUninstall(ctx context.Context, clusterID uint, name, ns string) (string, error) {
	args := []string{"uninstall", name}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	return e.Helm(ctx, clusterID, args...)
}
