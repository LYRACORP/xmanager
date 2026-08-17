package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/docker"
)

func (c *Catalog) registerDocker() {
	sid := objSchema(map[string]any{
		"server_id":    numProp("ID of the server"),
		"container_id": strProp("Container ID or name"),
	}, "server_id", "container_id")
	c.add(Tool{Name: "docker_start", Group: "docker", Risk: RiskWrite, Description: "Start a Docker container.", InputSchema: sid, Handler: dockerAction("start")})
	c.add(Tool{Name: "docker_stop", Group: "docker", Risk: RiskWrite, Description: "Stop a Docker container.", InputSchema: sid, Handler: dockerAction("stop")})
	c.add(Tool{Name: "docker_restart", Group: "docker", Risk: RiskWrite, Description: "Restart a Docker container.", InputSchema: sid, Handler: dockerAction("restart")})
	c.add(Tool{
		Name: "docker_logs", Group: "docker", Risk: RiskRead,
		Description: "Tail Docker container logs.",
		InputSchema: objSchema(map[string]any{
			"server_id":    numProp("ID of the server"),
			"container_id": strProp("Container ID or name"),
			"lines":        numProp("Number of lines (default 100)"),
		}, "server_id", "container_id"),
		Handler: toolDockerLogs,
	})
	c.add(Tool{
		Name: "docker_compose", Group: "docker", Risk: RiskWrite,
		Description: "Run docker compose up, down, build, or pull in a remote directory.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("ID of the server"),
			"dir":       strProp("Project directory on the server"),
			"action":    strProp("up, down, build, or pull"),
		}, "server_id", "dir", "action"),
		Handler: toolDockerCompose,
	})
	c.add(Tool{
		Name: "docker_prune", Group: "docker", Risk: RiskDestructive,
		Description: "Prune unused Docker images, volumes, networks, or build cache.",
		InputSchema: objSchema(map[string]any{
			"server_id": numProp("ID of the server"),
			"target":    strProp("images, volumes, networks, or build"),
		}, "server_id", "target"),
		Handler: toolDockerPrune,
	})
}

func dockerAction(action string) Handler {
	return func(_ context.Context, c *Catalog, args map[string]any) (string, error) {
		exec, err := c.Executor(argUint(args, "server_id"))
		if err != nil {
			return "", err
		}
		id := argStr(args, "container_id")
		if id == "" {
			return "", fmt.Errorf("container_id is required")
		}
		mgr := docker.NewManager(exec)
		var run error
		switch action {
		case "start":
			run = mgr.StartContainer(id)
		case "stop":
			run = mgr.StopContainer(id)
		case "restart":
			run = mgr.RestartContainer(id)
		}
		if run != nil {
			return "", run
		}
		return fmt.Sprintf("container %s %s", id, action), nil
	}
}

func toolDockerLogs(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	lines := argInt(args, "lines", 100)
	if lines <= 0 {
		lines = 100
	}
	out, err := docker.NewManager(exec).ContainerLogs(argStr(args, "container_id"), lines)
	if err != nil {
		return "", err
	}
	if len(out) > 16000 {
		out = out[len(out)-16000:]
	}
	return out, nil
}

func toolDockerCompose(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	dir := argStr(args, "dir")
	if dir == "" || strings.ContainsAny(dir, ";|&`$") {
		return "", fmt.Errorf("invalid dir")
	}
	mgr := docker.NewManager(exec)
	switch argStr(args, "action") {
	case "up":
		return "compose up", mgr.ComposeUp(dir)
	case "down":
		return "compose down", mgr.ComposeDown(dir)
	case "build":
		return "compose build", mgr.ComposeBuild(dir)
	case "pull":
		return "compose pull", mgr.ComposePull(dir)
	default:
		return "", fmt.Errorf("action must be up, down, build, or pull")
	}
}

func toolDockerPrune(_ context.Context, c *Catalog, args map[string]any) (string, error) {
	exec, err := c.Executor(argUint(args, "server_id"))
	if err != nil {
		return "", err
	}
	mgr := docker.NewManager(exec)
	switch argStr(args, "target") {
	case "images":
		return mgr.PruneImages()
	case "volumes":
		return mgr.PruneVolumes()
	case "networks":
		return mgr.PruneNetworks()
	case "build":
		return mgr.PruneBuildCache()
	default:
		return "", fmt.Errorf("target must be images, volumes, networks, or build")
	}
}
