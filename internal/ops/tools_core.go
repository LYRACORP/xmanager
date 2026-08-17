package ops

import (
	"context"
	"fmt"
	"time"

	bugsinksvc "github.com/lyracorp/xmanager/internal/services/bugsink"
	databasussvc "github.com/lyracorp/xmanager/internal/services/databasus"
	giteasvc "github.com/lyracorp/xmanager/internal/services/gitea"
	k8ssvc "github.com/lyracorp/xmanager/internal/services/k8s"
	kafkasvc "github.com/lyracorp/xmanager/internal/services/kafka"
	mailinboxsvc "github.com/lyracorp/xmanager/internal/services/mailinbox"
	mattersvc "github.com/lyracorp/xmanager/internal/services/mattermost"
	netdatasvc "github.com/lyracorp/xmanager/internal/services/netdata"
	powerdnssvc "github.com/lyracorp/xmanager/internal/services/powerdns"
	rabbitsvc "github.com/lyracorp/xmanager/internal/services/rabbitmq"
	registrysvc "github.com/lyracorp/xmanager/internal/services/registry"
	rustfssvc "github.com/lyracorp/xmanager/internal/services/rustfs"
	umamisvc "github.com/lyracorp/xmanager/internal/services/umami"
	uptimekumasvc "github.com/lyracorp/xmanager/internal/services/uptimekuma"
	webpanelsvc "github.com/lyracorp/xmanager/internal/services/webpanel"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

type togglable interface {
	Enable(exec *ssh.Executor, cfg map[string]string) error
	Disable(exec *ssh.Executor) error
}

func lookupService(serviceType string, db *gorm.DB, serverID uint) togglable {
	switch serviceType {
	case "gitea":
		return giteasvc.New(db, serverID)
	case "kafka":
		return kafkasvc.New(db, serverID)
	case "mattermost":
		return mattersvc.New(db, serverID)
	case "rabbitmq":
		return rabbitsvc.New(db, serverID)
	case "registry":
		return registrysvc.New(db, serverID)
	case "rustfs":
		return rustfssvc.New(db, serverID)
	case "bugsink", "sentry", "glitchtip":
		return bugsinksvc.New(db, serverID)
	case "netdata":
		return netdatasvc.New(db, serverID)
	case "umami":
		return umamisvc.New(db, serverID)
	case "powerdns":
		return powerdnssvc.New(db, serverID)
	case "mailinbox", "mail":
		return mailinboxsvc.New(db, serverID)
	case "uptimekuma", "uptime-kuma":
		return uptimekumasvc.New(db, serverID)
	case "databasus":
		return databasussvc.New(db, serverID)
	case "k8s", "kubernetes":
		return k8ssvc.New(db, serverID)
	case "webpanel", "web":
		wp := webpanelsvc.New(db, serverID)
		var srv storage.Server
		if db.First(&srv, serverID).Error == nil {
			wp.SetHost(srv.Host)
			wp.SetSSH(clientConfig(srv))
		}
		return wp
	default:
		return nil
	}
}

func (c *Catalog) registerCore() {
	c.add(Tool{
		Name:        "list_servers",
		Group:       "fleet",
		Risk:        RiskRead,
		Description: "List all managed servers (id, name, host, user, tags). Passwords are never returned.",
		InputSchema: objSchema(map[string]any{}),
		Handler:     toolListServers,
	})
	c.add(Tool{
		Name:        "get_server_metrics",
		Group:       "fleet",
		Risk:        RiskRead,
		Description: "Return the latest CPU/RAM/disk/network snapshot for a server.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("ID of the server")}, "server_id"),
		Handler:     toolGetServerMetrics,
	})
	c.add(Tool{
		Name:        "add_server",
		Group:       "fleet",
		Risk:        RiskWrite,
		Description: "Add a managed server record. SSH key path or password required to connect later.",
		InputSchema: objSchema(map[string]any{
			"name":         strProp("Display name"),
			"host":         strProp("Hostname or IP"),
			"port":         numProp("SSH port (default 22)"),
			"user":         strProp("SSH user"),
			"ssh_key_path": strProp("Path to private key on the XManager host"),
			"password":     strProp("SSH/sudo password (stored locally)"),
			"tags":         strProp("Comma-separated tags"),
			"jump_host":    strProp("Optional jump host"),
		}, "name", "host", "user"),
		Handler: toolAddServer,
	})
	c.add(Tool{
		Name:        "remove_server",
		Group:       "fleet",
		Risk:        RiskDestructive,
		Description: "Delete a managed server record and disconnect SSH.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("ID of the server")}, "server_id"),
		Handler:     toolRemoveServer,
	})
	c.add(Tool{
		Name:        "list_containers",
		Group:       "docker",
		Risk:        RiskRead,
		Description: "Run docker ps -a over SSH and return containers for a server.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("ID of the server")}, "server_id"),
		Handler:     toolListContainers,
	})
	c.add(Tool{
		Name:        "deploy_project",
		Group:       "projects",
		Risk:        RiskWrite,
		Description: "Deploy a project by ID using its configured strategy.",
		InputSchema: objSchema(map[string]any{"project_id": numProp("ID of the project")}, "project_id"),
		Handler:     toolDeployProject,
	})
	c.add(Tool{
		Name:        "run_script",
		Group:       "scripts",
		Risk:        RiskWrite,
		Description: "Execute a bash, python, or node script on one or more servers.",
		InputSchema: objSchema(map[string]any{
			"name":        strProp("Human-readable name for the run"),
			"script_type": strProp("Interpreter: bash, python, or node"),
			"content":     strProp("Script source code"),
			"server_ids":  arrIntProp("Server IDs; omit for all"),
			"all_servers": boolProp("Run on all servers"),
		}, "name", "script_type", "content"),
		Handler: toolRunScript,
	})
	c.add(Tool{
		Name:        "list_cron_jobs",
		Group:       "cron",
		Risk:        RiskRead,
		Description: "List scheduled cron jobs for a server.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("ID of the server")}, "server_id"),
		Handler:     toolListCronJobs,
	})
	c.add(Tool{
		Name:        "manage_cron",
		Group:       "cron",
		Risk:        RiskWrite,
		Description: "Add, remove, enable, or disable a cron job on a server.",
		InputSchema: objSchema(map[string]any{
			"action":     strProp("One of: add, remove, enable, disable"),
			"server_id":  numProp("ID of the server"),
			"job_id":     numProp("Job ID for remove/enable/disable"),
			"name":       strProp("Job name (add)"),
			"expression": strProp("5-field cron expression (add)"),
			"command":    strProp("Shell command (add)"),
		}, "action", "server_id"),
		Handler: toolManageCron,
	})
	c.add(Tool{
		Name:        "get_uptime",
		Group:       "uptime",
		Risk:        RiskRead,
		Description: "List uptime monitors. Pass server_id=0 for all.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("Filter by server ID; 0 = all")}),
		Handler:     toolGetUptime,
	})
	c.add(Tool{
		Name:        "list_projects",
		Group:       "projects",
		Risk:        RiskRead,
		Description: "List projects, optionally filtered by server.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("Filter by server ID; 0 = all")}),
		Handler:     toolListProjects,
	})
	c.add(Tool{
		Name:        "server_recon",
		Group:       "recon",
		Risk:        RiskRead,
		Description: "Port-scan a server via SSH (ss -tlnp).",
		InputSchema: objSchema(map[string]any{"server_id": numProp("ID of the server")}, "server_id"),
		Handler:     toolServerRecon,
	})
	c.add(Tool{
		Name:        "analyze_server",
		Group:       "recon",
		Risk:        RiskRead,
		Description: "Run full SSH recon scripts and return raw findings (OS, services, ports). Does not call an LLM.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("ID of the server")}, "server_id"),
		Handler:     toolAnalyzeServer,
	})
	c.add(Tool{
		Name:        "list_services",
		Group:       "services",
		Risk:        RiskRead,
		Description: "List ServiceInstance records for a server.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("ID of the server")}, "server_id"),
		Handler:     toolListServices,
	})
	c.add(Tool{
		Name:        "enable_service",
		Group:       "services",
		Risk:        RiskWrite,
		Description: "Enable a stack (gitea, registry, rustfs, powerdns, mailinbox, …) over SSH.",
		InputSchema: objSchema(map[string]any{
			"server_id":    numProp("ID of the server"),
			"service_type": strProp("Service type"),
		}, "server_id", "service_type"),
		Handler: func(ctx context.Context, cat *Catalog, args map[string]any) (string, error) {
			return toolToggleService(ctx, cat, args, true)
		},
	})
	c.add(Tool{
		Name:        "disable_service",
		Group:       "services",
		Risk:        RiskDestructive,
		Description: "Disable a stack and stop its containers.",
		InputSchema: objSchema(map[string]any{
			"server_id":    numProp("ID of the server"),
			"service_type": strProp("Service type"),
		}, "server_id", "service_type"),
		Handler: func(ctx context.Context, cat *Catalog, args map[string]any) (string, error) {
			return toolToggleService(ctx, cat, args, false)
		},
	})
	c.add(Tool{
		Name:        "run_workflow",
		Group:       "workflows",
		Risk:        RiskWrite,
		Description: "Run a saved workflow by ID (same engine as the canvas).",
		InputSchema: objSchema(map[string]any{"workflow_id": numProp("Workflow ID")}, "workflow_id"),
		Handler:     toolRunWorkflowPlaceholder,
	})
}

func toolListServers(_ context.Context, c *Catalog, _ map[string]any) (string, error) {
	var servers []storage.Server
	if err := c.DB.Find(&servers).Error; err != nil {
		return "", fmt.Errorf("querying servers: %w", err)
	}
	type view struct {
		ID       uint       `json:"id"`
		Name     string     `json:"name"`
		Host     string     `json:"host"`
		Port     int        `json:"port"`
		User     string     `json:"user"`
		Tags     string     `json:"tags"`
		JumpHost string     `json:"jump_host,omitempty"`
		LastSeen *time.Time `json:"last_seen,omitempty"`
	}
	out := make([]view, 0, len(servers))
	for _, s := range servers {
		out = append(out, view{ID: s.ID, Name: s.Name, Host: s.Host, Port: s.Port, User: s.User, Tags: s.Tags, JumpHost: s.JumpHost, LastSeen: s.LastSeen})
	}
	return jsonText(out)
}

func toolGetServerMetrics(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	serverID := argUint(args, "server_id")
	if serverID == 0 {
		return "", fmt.Errorf("server_id is required")
	}
	var snap storage.ServerMetricSnapshot
	if err := c.DB.Where("server_id = ?", serverID).Order("sampled_at DESC").First(&snap).Error; err != nil {
		return "", fmt.Errorf("no metrics for server %d: %w", serverID, err)
	}
	return jsonText(snap)
}

func toolAddServer(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	port := argInt(args, "port", 22)
	if port <= 0 {
		port = 22
	}
	srv := storage.Server{
		Name:       argStr(args, "name"),
		Host:       argStr(args, "host"),
		Port:       port,
		User:       argStr(args, "user"),
		SSHKeyPath: argStr(args, "ssh_key_path"),
		Password:   argStr(args, "password"),
		Tags:       argStr(args, "tags"),
		JumpHost:   argStr(args, "jump_host"),
	}
	if srv.Name == "" || srv.Host == "" || srv.User == "" {
		return "", fmt.Errorf("name, host, and user are required")
	}
	if err := c.DB.Create(&srv).Error; err != nil {
		return "", err
	}
	return jsonText(map[string]any{"id": srv.ID, "name": srv.Name, "host": srv.Host})
}

func toolRemoveServer(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	id := argUint(args, "server_id")
	if id == 0 {
		return "", fmt.Errorf("server_id is required")
	}
	if c.Pool != nil {
		c.Pool.Disconnect(id)
	}
	if err := c.DB.Delete(&storage.Server{}, id).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("server %d removed", id), nil
}

func toolRunWorkflowPlaceholder(_ context.Context, _ *Catalog, args map[string]any) (string, error) {
	id := argUint(args, "workflow_id")
	if id == 0 {
		return "", fmt.Errorf("workflow_id is required")
	}
	if runWorkflowFn == nil {
		return "", fmt.Errorf("workflow engine not wired")
	}
	return runWorkflowFn(id)
}

// runWorkflowFn is set by internal/workflow to avoid an import cycle.
var runWorkflowFn func(id uint) (string, error)

// SetWorkflowRunner registers the workflow engine callback.
func SetWorkflowRunner(fn func(id uint) (string, error)) {
	runWorkflowFn = fn
}
