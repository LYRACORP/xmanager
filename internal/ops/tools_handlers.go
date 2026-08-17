package ops

import (
	"context"
	"fmt"

	"github.com/lyracorp/xmanager/internal/cron"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/project"
	"github.com/lyracorp/xmanager/internal/recon"
	"github.com/lyracorp/xmanager/internal/scripts"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/uptime"
)

func toolListContainers(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	containers, err := docker.NewManager(exec).ListContainers()
	if err != nil {
		return "", fmt.Errorf("listing containers: %w", err)
	}
	return jsonText(containers)
}

func toolDeployProject(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	projectID := argUint(args, "project_id")
	if projectID == 0 {
		return "", fmt.Errorf("project_id is required")
	}
	var proj storage.Project
	if err := c.DB.First(&proj, projectID).Error; err != nil {
		return "", fmt.Errorf("project %d not found: %w", projectID, err)
	}
	if _, err := c.Executor(proj.ServerID); err != nil {
		return "", err
	}
	deployer, err := project.NewDeployerFromPool(c.Pool, proj.ServerID, c.DB)
	if err != nil {
		return "", err
	}
	res, err := deployer.Deploy(&proj)
	if err != nil {
		return "", fmt.Errorf("deploy failed: %w", err)
	}
	return jsonText(res)
}

func toolRunScript(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	name := argStr(args, "name")
	scriptType := argStr(args, "script_type")
	content := argStr(args, "content")
	if content == "" {
		return "", fmt.Errorf("content is required")
	}
	ids := argUintSlice(args, "server_ids")
	for _, id := range ids {
		if _, err := c.Executor(id); err != nil {
			return "", err
		}
	}
	if argBool(args, "all_servers") || len(ids) == 0 {
		var servers []storage.Server
		if err := c.DB.Find(&servers).Error; err != nil {
			return "", err
		}
		for _, s := range servers {
			if _, err := c.Executor(s.ID); err != nil {
				return "", fmt.Errorf("server %s: %w", s.Name, err)
			}
		}
	}
	runner := scripts.NewRunner(c.Pool, c.DB)
	if argBool(args, "all_servers") || len(ids) == 0 {
		return jsonText(runner.RunAll(name, scriptType, content))
	}
	if len(ids) == 1 {
		res, err := runner.Run(ids[0], name, scriptType, content)
		if err != nil {
			return "", err
		}
		return jsonText(res)
	}
	return jsonText(runner.RunMany(ids, name, scriptType, content))
}

func toolListCronJobs(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	var jobs []storage.CronJob
	q := c.DB
	if id := argUint(args, "server_id"); id > 0 {
		q = q.Where("server_id = ?", id)
	}
	if err := q.Find(&jobs).Error; err != nil {
		return "", fmt.Errorf("querying cron jobs: %w", err)
	}
	return jsonText(jobs)
}

func toolManageCron(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	action := argStr(args, "action")
	serverID := argUint(args, "server_id")
	if serverID == 0 {
		return "", fmt.Errorf("server_id is required")
	}
	exec, err := c.Executor(serverID)
	if err != nil {
		return "", err
	}
	mgr := cron.NewManager(exec, c.DB)
	switch action {
	case "add":
		jobName := argStr(args, "name")
		expr := argStr(args, "expression")
		cmd := argStr(args, "command")
		if jobName == "" || expr == "" || cmd == "" {
			return "", fmt.Errorf("name, expression, and command are required for add")
		}
		job, err := mgr.Add(serverID, jobName, expr, cmd)
		if err != nil {
			return "", err
		}
		return jsonText(job)
	case "remove":
		jobID := argUint(args, "job_id")
		if jobID == 0 {
			return "", fmt.Errorf("job_id is required for remove")
		}
		if err := mgr.Remove(jobID); err != nil {
			return "", err
		}
		return "cron job removed", nil
	case "enable", "disable":
		jobID := argUint(args, "job_id")
		if jobID == 0 {
			return "", fmt.Errorf("job_id is required")
		}
		if err := mgr.SetEnabled(jobID, action == "enable"); err != nil {
			return "", err
		}
		return "cron job " + action + "d", nil
	default:
		return "", fmt.Errorf("unknown action %q; expected: add, remove, enable, disable", action)
	}
}

func toolGetUptime(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	monitors, err := uptime.NewMonitor(c.DB).List(argUint(args, "server_id"))
	if err != nil {
		return "", fmt.Errorf("querying uptime monitors: %w", err)
	}
	return jsonText(monitors)
}

func toolListProjects(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	var projects []storage.Project
	q := c.DB
	if id := argUint(args, "server_id"); id > 0 {
		q = q.Where("server_id = ?", id)
	}
	if err := q.Find(&projects).Error; err != nil {
		return "", fmt.Errorf("querying projects: %w", err)
	}
	return jsonText(projects)
}

func toolServerRecon(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	ports, err := recon.ScanPorts(exec)
	if err != nil {
		return "", fmt.Errorf("port scan: %w", err)
	}
	return jsonText(ports)
}

func toolAnalyzeServer(ctx context.Context, c *Catalog, args map[string]any) (string, error) {
	serverID := argUint(args, "server_id")
	exec, err := c.Executor(serverID)
	if err != nil {
		return "", err
	}
	scan, err := recon.Scan(exec)
	if err != nil {
		return "", err
	}
	if len(scan.Raw) > 24000 {
		scan.Raw = scan.Raw[:24000] + "\n…truncated"
	}
	profile := scan.Raw
	if c.Completer != nil {
		analyzed, aerr := recon.Analyze(ctx, recon.Completer(c.Completer), scan)
		if aerr != nil {
			profile = scan.Raw + "\n\nAI analysis failed: " + aerr.Error()
		} else {
			profile = analyzed
		}
	}
	if err := recon.SaveProfile(c.DB, serverID, profile); err != nil {
		return profile, nil
	}
	return profile, nil
}

func toolListServices(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	var instances []storage.ServiceInstance
	q := c.DB
	if id := argUint(args, "server_id"); id > 0 {
		q = q.Where("server_id = ?", id)
	}
	if err := q.Find(&instances).Error; err != nil {
		return "", fmt.Errorf("querying service instances: %w", err)
	}
	return jsonText(instances)
}

func toolToggleService(_ context.Context, c *Catalog, args map[string]any, enable bool) (string, error) {
	serverID := argUint(args, "server_id")
	serviceType := argStr(args, "service_type")
	if serverID == 0 || serviceType == "" {
		return "", fmt.Errorf("server_id and service_type are required")
	}
	exec, err := c.Executor(serverID)
	if err != nil {
		return "", err
	}
	svc := lookupService(serviceType, c.DB, serverID)
	if svc == nil {
		return "", fmt.Errorf("unknown service_type %q", serviceType)
	}
	if enable {
		if err := svc.Enable(exec, nil); err != nil {
			return "", err
		}
	} else if err := svc.Disable(exec); err != nil {
		return "", err
	}
	status := "stopped"
	if enable {
		status = "running"
	}
	var inst storage.ServiceInstance
	res := c.DB.Where("server_id = ? AND service_type = ?", serverID, serviceType).First(&inst)
	if res.Error != nil {
		inst = storage.ServiceInstance{ServerID: serverID, ServiceType: serviceType, Enabled: enable, Status: status}
		_ = c.DB.Create(&inst).Error
	} else {
		_ = c.DB.Model(&inst).Updates(map[string]any{"enabled": enable, "status": status}).Error
	}
	action := "disabled"
	if enable {
		action = "enabled"
	}
	return fmt.Sprintf("service %s %s on server %d", serviceType, action, serverID), nil
}
