package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/cron"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/project"
	"github.com/lyracorp/xmanager/internal/recon"
	"github.com/lyracorp/xmanager/internal/scripts"
	bugsinksvc "github.com/lyracorp/xmanager/internal/services/bugsink"
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
	webpanelsvc "github.com/lyracorp/xmanager/internal/services/webpanel"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/uptime"
	"gorm.io/gorm"
)

// Options carries dependencies for the MCP server.
type Options struct {
	Config *config.Config
	DB     *gorm.DB
	Pool   *ssh.Pool
}

// RunStdio starts a newline-delimited JSON-RPC MCP server on stdin/stdout.
// It blocks until stdin is closed or an unrecoverable read error occurs.
func RunStdio(opts Options) error {
	s := &server{opts: opts}
	return s.serve(os.Stdin, os.Stdout)
}

// ── JSON-RPC 2.0 wire types ───────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ── MCP content / tool types ─────────────────────────────────────────────────

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// togglable is satisfied by all service implementations (gitea, kafka, …).
type togglable interface {
	Enable(exec *ssh.Executor, cfg map[string]string) error
	Disable(exec *ssh.Executor) error
}

// ── Server ───────────────────────────────────────────────────────────────────

type server struct {
	opts Options
}

func (s *server) serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}

		result, rpcErr := s.dispatch(req.Method, req.Params)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		_ = enc.Encode(resp)
	}
	return scanner.Err()
}

func (s *server) dispatch(method string, raw json.RawMessage) (interface{}, *rpcError) {
	switch method {
	case "initialize":
		return s.handleInitialize(raw)
	case "initialized":
		return struct{}{}, nil
	case "tools/list":
		return s.handleToolsList()
	case "tools/call":
		return s.handleToolsCall(raw)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

// ── initialize ────────────────────────────────────────────────────────────────

func (s *server) handleInitialize(_ json.RawMessage) (interface{}, *rpcError) {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "xmanager",
			"version": config.Version,
		},
	}, nil
}

// ── tools/list ────────────────────────────────────────────────────────────────

func (s *server) handleToolsList() (interface{}, *rpcError) {
	return map[string]interface{}{
		"tools": toolDefinitions(),
	}, nil
}

func toolDefinitions() []toolDef {
	obj := func(props map[string]interface{}, required ...string) interface{} {
		schema := map[string]interface{}{
			"type":       "object",
			"properties": props,
		}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	str := func(desc string) interface{} {
		return map[string]interface{}{"type": "string", "description": desc}
	}
	num := func(desc string) interface{} {
		return map[string]interface{}{"type": "integer", "description": desc}
	}
	arr := func(desc string) interface{} {
		return map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "integer"},
			"description": desc,
		}
	}
	bool_ := func(desc string) interface{} {
		return map[string]interface{}{"type": "boolean", "description": desc}
	}

	return []toolDef{
		{
			Name:        "list_servers",
			Description: "List all managed servers stored in the database.",
			InputSchema: obj(map[string]interface{}{}),
		},
		{
			Name:        "get_server_metrics",
			Description: "Return the latest ServerMetricSnapshot (CPU/RAM/disk/network) for a server.",
			InputSchema: obj(map[string]interface{}{
				"server_id": num("ID of the server"),
			}, "server_id"),
		},
		{
			Name:        "list_containers",
			Description: "Run docker ps -a over SSH and return all containers for a server.",
			InputSchema: obj(map[string]interface{}{
				"server_id": num("ID of the connected server"),
			}, "server_id"),
		},
		{
			Name:        "deploy_project",
			Description: "Deploy a project by its ID using the configured deployment strategy.",
			InputSchema: obj(map[string]interface{}{
				"project_id": num("ID of the project to deploy"),
			}, "project_id"),
		},
		{
			Name:        "run_script",
			Description: "Execute a bash, python, or node script on one or more servers.",
			InputSchema: obj(map[string]interface{}{
				"name":        str("Human-readable name for the run"),
				"script_type": str("Interpreter: bash, python, or node"),
				"content":     str("Script source code"),
				"server_ids":  arr("Server IDs to run on; omit or pass empty array for all connected servers"),
				"all_servers": bool_("Set true to run on all connected servers"),
			}, "name", "script_type", "content"),
		},
		{
			Name:        "list_cron_jobs",
			Description: "List scheduled cron jobs for a server.",
			InputSchema: obj(map[string]interface{}{
				"server_id": num("ID of the server"),
			}, "server_id"),
		},
		{
			Name:        "manage_cron",
			Description: "Add, remove, enable, or disable a cron job on a server.",
			InputSchema: obj(map[string]interface{}{
				"action":     str("One of: add, remove, enable, disable"),
				"server_id":  num("ID of the server (required for all actions)"),
				"job_id":     num("ID of the cron job (required for remove/enable/disable)"),
				"name":       str("Job name (required for add)"),
				"expression": str("5-field cron expression (required for add)"),
				"command":    str("Shell command to execute (required for add)"),
			}, "action", "server_id"),
		},
		{
			Name:        "get_uptime",
			Description: "List uptime monitors and their current last_status. Pass server_id=0 for all.",
			InputSchema: obj(map[string]interface{}{
				"server_id": num("Filter by server ID; 0 returns all monitors"),
			}),
		},
		{
			Name:        "list_projects",
			Description: "List projects, optionally filtered by server.",
			InputSchema: obj(map[string]interface{}{
				"server_id": num("Filter by server ID; 0 returns all projects"),
			}),
		},
		{
			Name:        "server_recon",
			Description: "Port-scan a server via SSH (ss -tlnp) and return listening ports.",
			InputSchema: obj(map[string]interface{}{
				"server_id": num("ID of the connected server"),
			}, "server_id"),
		},
		{
			Name:        "list_services",
			Description: "List ServiceInstance records (gitea, kafka, etc.) for a server.",
			InputSchema: obj(map[string]interface{}{
				"server_id": num("ID of the server"),
			}, "server_id"),
		},
		{
			Name:        "enable_service",
			Description: "Enable a service on a server (updates DB and calls Enable via SSH if connected).",
			InputSchema: obj(map[string]interface{}{
				"server_id":    num("ID of the server"),
				"service_type": str("Service type: gitea, kafka, mattermost, rabbitmq, registry, rustfs"),
			}, "server_id", "service_type"),
		},
		{
			Name:        "disable_service",
			Description: "Disable a service on a server (updates DB and calls Disable via SSH if connected).",
			InputSchema: obj(map[string]interface{}{
				"server_id":    num("ID of the server"),
				"service_type": str("Service type: gitea, kafka, mattermost, rabbitmq, registry, rustfs"),
			}, "server_id", "service_type"),
		},
	}
}

// ── tools/call ────────────────────────────────────────────────────────────────

type callParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func (s *server) handleToolsCall(raw json.RawMessage) (interface{}, *rpcError) {
	var p callParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
	}
	if p.Arguments == nil {
		p.Arguments = map[string]interface{}{}
	}

	result, err := s.callTool(p.Name, p.Arguments)
	if err != nil {
		return callResult{
			Content: []textContent{{Type: "text", Text: fmt.Sprintf("error: %v", err)}},
			IsError: true,
		}, nil
	}
	return result, nil
}

func (s *server) callTool(name string, args map[string]interface{}) (callResult, error) {
	switch name {
	case "list_servers":
		return s.toolListServers()
	case "get_server_metrics":
		return s.toolGetServerMetrics(argUint(args, "server_id"))
	case "list_containers":
		return s.toolListContainers(argUint(args, "server_id"))
	case "deploy_project":
		return s.toolDeployProject(argUint(args, "project_id"))
	case "run_script":
		return s.toolRunScript(args)
	case "list_cron_jobs":
		return s.toolListCronJobs(argUint(args, "server_id"))
	case "manage_cron":
		return s.toolManageCron(args)
	case "get_uptime":
		return s.toolGetUptime(argUint(args, "server_id"))
	case "list_projects":
		return s.toolListProjects(argUint(args, "server_id"))
	case "server_recon":
		return s.toolServerRecon(argUint(args, "server_id"))
	case "list_services":
		return s.toolListServices(argUint(args, "server_id"))
	case "enable_service":
		return s.toolToggleService(argUint(args, "server_id"), argStr(args, "service_type"), true)
	case "disable_service":
		return s.toolToggleService(argUint(args, "server_id"), argStr(args, "service_type"), false)
	default:
		return callResult{}, fmt.Errorf("unknown tool: %s", name)
	}
}

// ── Tool implementations ──────────────────────────────────────────────────────

func (s *server) toolListServers() (callResult, error) {
	var servers []storage.Server
	if err := s.opts.DB.Find(&servers).Error; err != nil {
		return callResult{}, fmt.Errorf("querying servers: %w", err)
	}
	return jsonResult(servers)
}

func (s *server) toolGetServerMetrics(serverID uint) (callResult, error) {
	if serverID == 0 {
		return callResult{}, fmt.Errorf("server_id is required")
	}
	var snap storage.ServerMetricSnapshot
	if err := s.opts.DB.Where("server_id = ?", serverID).
		Order("sampled_at DESC").First(&snap).Error; err != nil {
		return callResult{}, fmt.Errorf("no metrics for server %d: %w", serverID, err)
	}
	return jsonResult(snap)
}

func (s *server) toolListContainers(serverID uint) (callResult, error) {
	exec, ok := s.opts.Pool.GetExecutor(serverID)
	if !ok {
		return callResult{}, fmt.Errorf("server %d not connected", serverID)
	}
	mgr := docker.NewManager(exec)
	containers, err := mgr.ListContainers()
	if err != nil {
		return callResult{}, fmt.Errorf("listing containers: %w", err)
	}
	return jsonResult(containers)
}

func (s *server) toolDeployProject(projectID uint) (callResult, error) {
	if projectID == 0 {
		return callResult{}, fmt.Errorf("project_id is required")
	}
	var proj storage.Project
	if err := s.opts.DB.First(&proj, projectID).Error; err != nil {
		return callResult{}, fmt.Errorf("project %d not found: %w", projectID, err)
	}
	deployer, err := project.NewDeployerFromPool(s.opts.Pool, proj.ServerID, s.opts.DB)
	if err != nil {
		return callResult{}, err
	}
	res, err := deployer.Deploy(&proj)
	if err != nil {
		return callResult{}, fmt.Errorf("deploy failed: %w", err)
	}
	return jsonResult(res)
}

func (s *server) toolRunScript(args map[string]interface{}) (callResult, error) {
	name := argStr(args, "name")
	scriptType := argStr(args, "script_type")
	content := argStr(args, "content")

	if content == "" {
		return callResult{}, fmt.Errorf("content is required")
	}

	runner := scripts.NewRunner(s.opts.Pool, s.opts.DB)

	if argBool(args, "all_servers") {
		results := runner.RunAll(name, scriptType, content)
		return jsonResult(results)
	}

	ids := argUintSlice(args, "server_ids")
	if len(ids) == 0 {
		results := runner.RunAll(name, scriptType, content)
		return jsonResult(results)
	}
	if len(ids) == 1 {
		res, err := runner.Run(ids[0], name, scriptType, content)
		if err != nil {
			return callResult{}, err
		}
		return jsonResult(res)
	}
	results := runner.RunMany(ids, name, scriptType, content)
	return jsonResult(results)
}

func (s *server) toolListCronJobs(serverID uint) (callResult, error) {
	var jobs []storage.CronJob
	q := s.opts.DB
	if serverID > 0 {
		q = q.Where("server_id = ?", serverID)
	}
	if err := q.Find(&jobs).Error; err != nil {
		return callResult{}, fmt.Errorf("querying cron jobs: %w", err)
	}
	return jsonResult(jobs)
}

func (s *server) toolManageCron(args map[string]interface{}) (callResult, error) {
	action := argStr(args, "action")
	serverID := argUint(args, "server_id")

	if serverID == 0 {
		return callResult{}, fmt.Errorf("server_id is required")
	}

	exec, ok := s.opts.Pool.GetExecutor(serverID)
	if !ok {
		return callResult{}, fmt.Errorf("server %d not connected", serverID)
	}
	mgr := cron.NewManager(exec, s.opts.DB)

	switch action {
	case "add":
		jobName := argStr(args, "name")
		expr := argStr(args, "expression")
		cmd := argStr(args, "command")
		if jobName == "" || expr == "" || cmd == "" {
			return callResult{}, fmt.Errorf("name, expression, and command are required for add")
		}
		job, err := mgr.Add(serverID, jobName, expr, cmd)
		if err != nil {
			return callResult{}, err
		}
		return jsonResult(job)

	case "remove":
		jobID := argUint(args, "job_id")
		if jobID == 0 {
			return callResult{}, fmt.Errorf("job_id is required for remove")
		}
		if err := mgr.Remove(jobID); err != nil {
			return callResult{}, err
		}
		return textResult("cron job removed")

	case "enable":
		jobID := argUint(args, "job_id")
		if jobID == 0 {
			return callResult{}, fmt.Errorf("job_id is required for enable")
		}
		if err := mgr.SetEnabled(jobID, true); err != nil {
			return callResult{}, err
		}
		return textResult("cron job enabled")

	case "disable":
		jobID := argUint(args, "job_id")
		if jobID == 0 {
			return callResult{}, fmt.Errorf("job_id is required for disable")
		}
		if err := mgr.SetEnabled(jobID, false); err != nil {
			return callResult{}, err
		}
		return textResult("cron job disabled")

	default:
		return callResult{}, fmt.Errorf("unknown action %q; expected: add, remove, enable, disable", action)
	}
}

func (s *server) toolGetUptime(serverID uint) (callResult, error) {
	mon := uptime.NewMonitor(s.opts.DB)
	monitors, err := mon.List(serverID)
	if err != nil {
		return callResult{}, fmt.Errorf("querying uptime monitors: %w", err)
	}
	return jsonResult(monitors)
}

func (s *server) toolListProjects(serverID uint) (callResult, error) {
	var projects []storage.Project
	q := s.opts.DB
	if serverID > 0 {
		q = q.Where("server_id = ?", serverID)
	}
	if err := q.Find(&projects).Error; err != nil {
		return callResult{}, fmt.Errorf("querying projects: %w", err)
	}
	return jsonResult(projects)
}

func (s *server) toolServerRecon(serverID uint) (callResult, error) {
	exec, ok := s.opts.Pool.GetExecutor(serverID)
	if !ok {
		return callResult{}, fmt.Errorf("server %d not connected", serverID)
	}
	ports, err := recon.ScanPorts(exec)
	if err != nil {
		return callResult{}, fmt.Errorf("port scan: %w", err)
	}
	return jsonResult(ports)
}

func (s *server) toolListServices(serverID uint) (callResult, error) {
	var instances []storage.ServiceInstance
	q := s.opts.DB
	if serverID > 0 {
		q = q.Where("server_id = ?", serverID)
	}
	if err := q.Find(&instances).Error; err != nil {
		return callResult{}, fmt.Errorf("querying service instances: %w", err)
	}
	return jsonResult(instances)
}

func (s *server) toolToggleService(serverID uint, serviceType string, enable bool) (callResult, error) {
	if serverID == 0 || serviceType == "" {
		return callResult{}, fmt.Errorf("server_id and service_type are required")
	}

	var inst storage.ServiceInstance
	if err := s.opts.DB.Where("server_id = ? AND service_type = ?", serverID, serviceType).
		First(&inst).Error; err != nil {
		return callResult{}, fmt.Errorf("service instance not found: %w", err)
	}

	if exec, ok := s.opts.Pool.GetExecutor(serverID); ok {
		svc := lookupService(serviceType, s.opts.DB, serverID)
		if svc != nil {
			if enable {
				_ = svc.Enable(exec, nil)
			} else {
				_ = svc.Disable(exec)
			}
		}
	}

	status := "stopped"
	if enable {
		status = "running"
	}
	_ = s.opts.DB.Model(&inst).Updates(map[string]interface{}{
		"enabled": enable,
		"status":  status,
	}).Error

	action := "disabled"
	if enable {
		action = "enabled"
	}
	return textResult(fmt.Sprintf("service %s %s on server %d", serviceType, action, serverID))
}

// lookupService returns the service implementation for the given service type,
// or nil if unknown.
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
	case "k8s", "kubernetes":
		return k8ssvc.New(db, serverID)
	case "webpanel", "web":
		wp := webpanelsvc.New(db, serverID)
		var srv storage.Server
		if db.First(&srv, serverID).Error == nil {
			wp.SetHost(srv.Host)
		}
		return wp
	default:
		return nil
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func jsonResult(v interface{}) (callResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return callResult{}, fmt.Errorf("marshaling result: %w", err)
	}
	return callResult{Content: []textContent{{Type: "text", Text: string(data)}}}, nil
}

func textResult(text string) (callResult, error) {
	return callResult{Content: []textContent{{Type: "text", Text: text}}}, nil
}

func argUint(args map[string]interface{}, key string) uint {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return uint(x)
	case json.Number:
		n, _ := x.Int64()
		return uint(n)
	case string:
		n, _ := strconv.ParseUint(x, 10, 64)
		return uint(n)
	case int:
		return uint(x)
	case int64:
		return uint(x)
	}
	return 0
}

func argStr(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func argBool(args map[string]interface{}, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func argUintSlice(args map[string]interface{}, key string) []uint {
	v, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]uint, 0, len(arr))
	for _, item := range arr {
		result = append(result, argUint(map[string]interface{}{"x": item}, "x"))
	}
	return result
}
