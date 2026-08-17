package ops

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lyracorp/xmanager/internal/backup"
	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/pm2"
	"github.com/lyracorp/xmanager/internal/recipes"
	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

func (c *Catalog) registerFiles() {
	c.add(Tool{
		Name: "list_files", Group: "files", Risk: RiskRead,
		Description: "List a remote directory over SFTP.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"path":      strProp("Absolute directory path"),
		}, "server_id", "path"),
		Handler: toolListFiles,
	})
	c.add(Tool{
		Name: "read_file", Group: "files", Risk: RiskRead,
		Description: "Read a remote text file (capped at 64KiB).",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"path":      strProp("Absolute file path"),
		}, "server_id", "path"),
		Handler: toolReadFile,
	})
	c.add(Tool{
		Name: "write_file", Group: "files", Risk: RiskWrite,
		Description: "Write a remote text file over SFTP.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"path":      strProp("Absolute file path"),
			"content":   strProp("File contents"),
		}, "server_id", "path", "content"),
		Handler: toolWriteFile,
	})
}

func (c *Catalog) registerPackages() {
	c.add(Tool{
		Name: "list_recipes", Group: "packages", Risk: RiskRead,
		Description: "List installable host recipes (docker, nodejs, linux-harden, …).",
		InputSchema: objSchema(map[string]any{}),
		Handler:     toolListRecipes,
	})
	c.add(Tool{
		Name: "install_recipe", Group: "packages", Risk: RiskWrite,
		Description: "Install a recipe on a server over SSH.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"recipe_id": strProp("Recipe id from list_recipes"),
		}, "server_id", "recipe_id"),
		Handler: toolInstallRecipe,
	})
	c.add(Tool{
		Name: "uninstall_recipe", Group: "packages", Risk: RiskDestructive,
		Description: "Uninstall a recipe from a server.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"recipe_id": strProp("Recipe id"),
		}, "server_id", "recipe_id"),
		Handler: toolUninstallRecipe,
	})
}

func (c *Catalog) registerDatabases() {
	c.add(Tool{
		Name: "list_databases", Group: "databases", Risk: RiskRead,
		Description: "List databases for an engine (postgres, mysql, mariadb, mongodb, clickhouse, redis).",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"engine":    strProp("postgres, mysql, mariadb, mongodb, clickhouse, redis"),
		}, "server_id", "engine"),
		Handler: toolListDatabases,
	})
	c.add(Tool{
		Name: "create_database", Group: "databases", Risk: RiskWrite,
		Description: "Create a database on the given engine.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"engine":    strProp("Engine type"),
			"name":      strProp("Database name"),
		}, "server_id", "engine", "name"),
		Handler: toolCreateDatabase,
	})
	c.add(Tool{
		Name: "drop_database", Group: "databases", Risk: RiskDestructive,
		Description: "Drop a database.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"engine":    strProp("Engine type"),
			"name":      strProp("Database name"),
		}, "server_id", "engine", "name"),
		Handler: toolDropDatabase,
	})
}

func (c *Catalog) registerBackup() {
	c.add(Tool{
		Name: "list_backups", Group: "backup", Risk: RiskRead,
		Description: "List backup records for a server.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("Server ID")}, "server_id"),
		Handler:     toolListBackups,
	})
	c.add(Tool{
		Name: "run_backup", Group: "backup", Risk: RiskWrite,
		Description: "Run a stored backup schedule by ID.",
		InputSchema: objSchema(map[string]any{"backup_id": numProp("Backup record ID")}, "backup_id"),
		Handler:     toolRunBackup,
	})
}

func (c *Catalog) registerSecurity() {
	c.add(Tool{
		Name: "firewall_status", Group: "security", Risk: RiskRead,
		Description: "Read UFW/iptables firewall status.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("Server ID")}, "server_id"),
		Handler:     toolFirewallStatus,
	})
	c.add(Tool{
		Name: "firewall_allow", Group: "security", Risk: RiskWrite,
		Description: "Allow ports through UFW (e.g. 80,443 or 8080/tcp).",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"ports":     strProp("Port spec"),
		}, "server_id", "ports"),
		Handler: toolFirewallAllow,
	})
	c.add(Tool{
		Name: "ssh_harden", Group: "security", Risk: RiskDestructive,
		Description: "Apply SSH hardening drop-in (disables password/root login). Confirm before use.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("Server ID")}, "server_id"),
		Handler:     toolSSHHarden,
	})
	c.add(Tool{
		Name: "ssl_renew", Group: "security", Risk: RiskWrite,
		Description: "Renew Let's Encrypt certificates via certbot.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("Server ID")}, "server_id"),
		Handler:     toolSSLRenew,
	})
}

func (c *Catalog) registerDomains() {
	c.add(Tool{
		Name: "list_domains", Group: "domains", Risk: RiskRead,
		Description: "List connected domains for a server.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("Server ID")}, "server_id"),
		Handler:     toolListDomains,
	})
	c.add(Tool{
		Name: "add_domain", Group: "domains", Risk: RiskWrite,
		Description: "Record a connected domain on a server.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"domain":    strProp("Hostname"),
		}, "server_id", "domain"),
		Handler: toolAddDomain,
	})
	c.add(Tool{
		Name: "list_ftp_users", Group: "ftp", Risk: RiskRead,
		Description: "List FTP users for a server.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("Server ID")}, "server_id"),
		Handler:     toolListFTP,
	})
}

func (c *Catalog) registerProcess() {
	c.add(Tool{
		Name: "pm2_list", Group: "process", Risk: RiskRead,
		Description: "List PM2 processes.",
		InputSchema: objSchema(map[string]any{"server_id": numProp("Server ID")}, "server_id"),
		Handler:     toolPM2List,
	})
	c.add(Tool{
		Name: "pm2_restart", Group: "process", Risk: RiskWrite,
		Description: "Restart a PM2 process by id.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"pm_id":     numProp("PM2 id"),
		}, "server_id", "pm_id"),
		Handler: toolPM2Restart,
	})
	c.add(Tool{
		Name: "systemd_unit", Group: "process", Risk: RiskWrite,
		Description: "systemctl start, stop, restart, or status a unit.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("Server ID"),
			"unit":      strProp("Unit name, e.g. nginx.service"),
			"action":    strProp("start, stop, restart, or status"),
		}, "server_id", "unit", "action"),
		Handler: toolSystemd,
	})
}

func toolListFiles(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	serverID := argUint(args, "server_id")
	path := argStr(args, "path")
	if path == "" {
		path = "/"
	}
	if _, err := c.Executor(serverID); err != nil {
		return "", err
	}
	var names []string
	err := c.Pool.WithSFTP(serverID, func(sftp *ssh.SFTPClient) error {
		ents, err := sftp.ListDir(path)
		if err != nil {
			return err
		}
		for _, e := range ents {
			mark := ""
			if e.IsDir() {
				mark = "/"
			}
			names = append(names, e.Name()+mark)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return jsonText(names)
}

func toolReadFile(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	serverID := argUint(args, "server_id")
	path := argStr(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if _, err := c.Executor(serverID); err != nil {
		return "", err
	}
	var data []byte
	err := c.Pool.WithSFTP(serverID, func(sftp *ssh.SFTPClient) error {
		b, err := sftp.ReadFile(path)
		if err != nil {
			return err
		}
		const max = 64 * 1024
		if len(b) > max {
			b = b[:max]
		}
		data = b
		return nil
	})
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("file is not valid UTF-8")
	}
	return string(data), nil
}

func toolWriteFile(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	serverID := argUint(args, "server_id")
	path := argStr(args, "path")
	content := argStr(args, "content")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if _, err := c.Executor(serverID); err != nil {
		return "", err
	}
	err := c.Pool.WithSFTP(serverID, func(sftp *ssh.SFTPClient) error {
		return sftp.WriteFile(path, []byte(content), 0644)
	})
	if err != nil {
		return "", err
	}
	return "wrote " + path, nil
}

func toolListRecipes(_ context.Context, _ *Catalog, _ map[string]any) (string, error) {
	all, err := recipes.All()
	if err != nil {
		return "", err
	}
	type row struct {
		ID, Name, Description string
		Destructive           bool
	}
	out := make([]row, 0, len(all))
	for _, r := range all {
		out = append(out, row{ID: r.ID, Name: r.Name, Description: r.Description, Destructive: r.Destructive})
	}
	return jsonText(out)
}

func recipeRunner(c *Catalog, serverID uint) (recipes.Runner, error) {
	exec, err := c.Executor(serverID)
	if err != nil {
		return recipes.Runner{}, err
	}
	srv, err := c.loadServer(serverID)
	if err != nil {
		return recipes.Runner{}, err
	}
	return recipes.Runner{Exec: exec, User: srv.User, Password: srv.Password}, nil
}

func toolInstallRecipe(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	r, ok := recipes.ByID(argStr(args, "recipe_id"))
	if !ok {
		return "", fmt.Errorf("unknown recipe")
	}
	run, err := recipeRunner(c, argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	res := run.Run(r, nil)
	if !res.OK {
		if res.Err != nil {
			return "", res.Err
		}
		return "", fmt.Errorf("recipe failed: %s", res.Output)
	}
	return res.Output, nil
}

func toolUninstallRecipe(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	r, ok := recipes.ByID(argStr(args, "recipe_id"))
	if !ok {
		return "", fmt.Errorf("unknown recipe")
	}
	run, err := recipeRunner(c, argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	res := run.Uninstall(r, nil)
	if !res.OK {
		if res.Err != nil {
			return "", res.Err
		}
		return "", fmt.Errorf("uninstall failed: %s", res.Output)
	}
	return res.Output, nil
}

func dbMgr(c *Catalog, args map[string]any) (dbmanager.Manager, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return nil, err
	}
	engine := dbmanager.DBType(strings.ToLower(argStr(args, "engine")))
	mgr := dbmanager.NewManager(engine, exec)
	if mgr == nil {
		return nil, fmt.Errorf("unknown engine")
	}
	return mgr, nil
}

func toolListDatabases(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	mgr, err := dbMgr(c, args)
	if err != nil {
		return "", err
	}
	dbs, err := mgr.ListDatabases()
	if err != nil {
		return "", err
	}
	return jsonText(dbs)
}

func toolCreateDatabase(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	mgr, err := dbMgr(c, args)
	if err != nil {
		return "", err
	}
	if err := mgr.CreateDatabase(argStr(args, "name")); err != nil {
		return "", err
	}
	return "database created", nil
}

func toolDropDatabase(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	mgr, err := dbMgr(c, args)
	if err != nil {
		return "", err
	}
	if err := mgr.DropDatabase(argStr(args, "name")); err != nil {
		return "", err
	}
	return "database dropped", nil
}

func toolListBackups(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	list, err := backup.NewScheduler(c.DB).ListBackups(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	return jsonText(list)
}

func toolRunBackup(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	var job storage.Backup
	if err := c.DB.First(&job, argUint(args, "backup_id")).Error; err != nil {
		return "", err
	}
	exec, err := c.Executor(job.ServerID)
	if err != nil {
		return "", err
	}
	backup.NewScheduler(c.DB).RunSchedule(exec, job, job.BackedAt)
	return "backup started", nil
}

func toolFirewallStatus(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	return jsonText(security.ReadFirewall(exec))
}

func toolFirewallAllow(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	if err := security.AllowPorts(exec, argStr(args, "ports")); err != nil {
		return "", err
	}
	return "ports allowed", nil
}

func toolSSHHarden(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	if err := security.ApplySSHHardening(exec); err != nil {
		return "", err
	}
	return "ssh hardening applied", nil
}

func toolSSLRenew(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	return security.RenewSSL(exec)
}

func toolListDomains(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	var rows []storage.ConnectedDomain
	q := c.DB
	if id := argUint(args, "server_id"); id > 0 {
		q = q.Where("server_id = ?", id)
	}
	if err := q.Find(&rows).Error; err != nil {
		return "", err
	}
	return jsonText(rows)
}

func toolAddDomain(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	row := storage.ConnectedDomain{ServerID: argUint(args, "server_id"), Domain: argStr(args, "domain")}
	if row.ServerID == 0 || row.Domain == "" {
		return "", fmt.Errorf("server_id and domain are required")
	}
	if err := c.DB.Create(&row).Error; err != nil {
		return "", err
	}
	return jsonText(row)
}

func toolListFTP(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	var rows []storage.FTPUser
	q := c.DB
	if id := argUint(args, "server_id"); id > 0 {
		q = q.Where("server_id = ?", id)
	}
	if err := q.Find(&rows).Error; err != nil {
		return "", err
	}
	return jsonText(rows)
}

func toolPM2List(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	list, err := pm2.NewManager(exec).List()
	if err != nil {
		return "", err
	}
	return jsonText(list)
}

func toolPM2Restart(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	if err := pm2.NewManager(exec).Restart(argInt(args, "pm_id", -1)); err != nil {
		return "", err
	}
	return "pm2 restarted", nil
}

func toolSystemd(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	unit := argStr(args, "unit")
	action := argStr(args, "action")
	if unit == "" || strings.ContainsAny(unit, " ;|&") {
		return "", fmt.Errorf("invalid unit")
	}
	switch action {
	case "start", "stop", "restart", "status":
	default:
		return "", fmt.Errorf("action must be start, stop, restart, or status")
	}
	res, err := exec.Run("systemctl " + action + " " + unit)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	if res.ExitCode != 0 {
		return "", fmt.Errorf("systemctl: %s", out)
	}
	if out == "" {
		return action + " " + unit, nil
	}
	return out, nil
}
