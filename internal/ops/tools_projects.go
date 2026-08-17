package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/storage"
)

func (c *Catalog) registerProjects() {
	c.add(Tool{
		Name: "create_project", Group: "projects", Risk: RiskWrite,
		Description: "Create a project record (image, compose, dockerfile, git, oneclick, archive, …).",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"name":      strProp("Project name"),
			"type":      strProp("image, compose, dockerfile, git, oneclick, clone, push, scratch, archive, function"),
			"source":    strProp("Image, git URL, or path"),
		}, "server_id", "name", "type"),
		Handler: toolCreateProject,
	})
	c.add(Tool{
		Name: "stop_project", Group: "projects", Risk: RiskWrite,
		Description: "Stop a project's containers (compose down / docker rm).",
		InputSchema: objSchema(map[string]any{"project_id": numProp("Project ID")}, "project_id"),
		Handler:     toolStopProject,
	})
	c.add(Tool{
		Name: "restart_project", Group: "projects", Risk: RiskWrite,
		Description: "Restart a project's containers.",
		InputSchema: objSchema(map[string]any{"project_id": numProp("Project ID")}, "project_id"),
		Handler:     toolRestartProject,
	})
	c.add(Tool{
		Name: "set_project_env", Group: "projects", Risk: RiskWrite,
		Description: "Set or update a project environment variable.",
		InputSchema: objSchema(map[string]any{
			"project_id": numProp("Project ID"),
			"key":        strProp("Env var name"),
			"value":      strProp("Env var value"),
		}, "project_id", "key"),
		Handler: toolSetProjectEnv,
	})
	c.add(Tool{
		Name: "set_project_domain", Group: "projects", Risk: RiskWrite,
		Description: "Attach a hostname to a project.",
		InputSchema: objSchema(map[string]any{
			"project_id": numProp("Project ID"),
			"domain":     strProp("Hostname"),
		}, "project_id", "domain"),
		Handler: toolSetProjectDomain,
	})
}

func slugName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == ' ' || r == '_' {
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "project"
	}
	return s
}

func toolCreateProject(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	p := storage.Project{
		ServerID: argUint(args, "server_id"),
		Name:     argStr(args, "name"),
		Type:     argStr(args, "type"),
		Source:   argStr(args, "source"),
	}
	if p.ServerID == 0 || p.Name == "" || p.Type == "" {
		return "", fmt.Errorf("server_id, name, and type are required")
	}
	if err := c.DB.Create(&p).Error; err != nil {
		return "", err
	}
	return jsonText(map[string]any{"id": p.ID, "name": p.Name, "type": p.Type})
}

func toolStopProject(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	var p storage.Project
	if err := c.DB.First(&p, argUint(args, "project_id")).Error; err != nil {
		return "", err
	}
	exec, err := c.Executor(p.ServerID)
	if err != nil {
		return "", err
	}
	name := slugName(p.Name)
	_, _ = exec.Run(fmt.Sprintf(`docker rm -f %s 2>/dev/null; cd /opt/xmanager/projects/%s && docker compose down 2>/dev/null || true`, name, name))
	_ = c.DB.Model(&p).Update("deploy_status", "stopped").Error
	return "project stopped", nil
}

func toolRestartProject(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	var p storage.Project
	if err := c.DB.First(&p, argUint(args, "project_id")).Error; err != nil {
		return "", err
	}
	exec, err := c.Executor(p.ServerID)
	if err != nil {
		return "", err
	}
	name := slugName(p.Name)
	_, _ = exec.Run(fmt.Sprintf(`cd /opt/xmanager/projects/%s 2>/dev/null && docker compose restart 2>/dev/null || docker restart %s 2>/dev/null || true`, name, name))
	return "project restarted", nil
}

func toolSetProjectEnv(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	pid := argUint(args, "project_id")
	key := argStr(args, "key")
	if pid == 0 || key == "" {
		return "", fmt.Errorf("project_id and key are required")
	}
	val := argStr(args, "value")
	var ev storage.ProjectEnvVar
	err := c.DB.Where("project_id = ? AND key = ?", pid, key).First(&ev).Error
	if err != nil {
		ev = storage.ProjectEnvVar{ProjectID: pid, Key: key, ValueEncrypted: val}
		if err := c.DB.Create(&ev).Error; err != nil {
			return "", err
		}
	} else {
		ev.ValueEncrypted = val
		if err := c.DB.Save(&ev).Error; err != nil {
			return "", err
		}
	}
	return "env updated", nil
}

func toolSetProjectDomain(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	d := storage.ProjectDomain{ProjectID: argUint(args, "project_id"), Domain: argStr(args, "domain")}
	if d.ProjectID == 0 || d.Domain == "" {
		return "", fmt.Errorf("project_id and domain are required")
	}
	if err := c.DB.Create(&d).Error; err != nil {
		return "", err
	}
	return jsonText(d)
}
