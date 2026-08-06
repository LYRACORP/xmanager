package project

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

type Deployer struct {
	exec *ssh.Executor
	pool *ssh.Pool
	db   *gorm.DB
}

func NewDeployer(exec *ssh.Executor, db *gorm.DB) *Deployer {
	return &Deployer{exec: exec, db: db}
}

// NewDeployerFromPool constructs a Deployer from the pool for a given server.
func NewDeployerFromPool(pool *ssh.Pool, serverID uint, db *gorm.DB) (*Deployer, error) {
	exec, ok := pool.GetExecutor(serverID)
	if !ok {
		return nil, fmt.Errorf("server %d not connected", serverID)
	}
	return &Deployer{exec: exec, pool: pool, db: db}, nil
}

// Deploy dispatches to the right deployment strategy based on project type.
func (d *Deployer) Deploy(proj *storage.Project) (*DeployResult, error) {
	var cfg Config
	if proj.ConfigJSON != "" {
		if err := json.Unmarshal([]byte(proj.ConfigJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parsing project config: %w", err)
		}
	}

	_ = d.db.Model(proj).Update("deploy_status", StatusBuilding).Error

	var (
		result *DeployResult
		err    error
	)

	switch Type(proj.Type) {
	case TypeImage:
		result, err = d.deployImage(proj, &cfg)
	case TypeCompose:
		result, err = d.deployCompose(proj, &cfg)
	case TypeDockerfile:
		result, err = d.deployDockerfile(proj, &cfg)
	case TypeGit:
		result, err = d.deployGit(proj, &cfg)
	case TypeOneClick:
		result, err = d.deployOneClick(proj, &cfg)
	case TypeClone:
		result, err = d.deployClone(proj, &cfg)
	case TypePush:
		result, err = d.deployPush(proj, &cfg)
	case TypeScratch:
		result, err = d.deployScratch(proj, &cfg)
	case TypeArchive:
		result, err = d.deployArchive(proj, &cfg)
	case TypeFunction:
		result, err = d.deployFunction(proj, &cfg)
	default:
		err = fmt.Errorf("unknown project type: %s", proj.Type)
	}

	status := StatusRunning
	output := ""
	if err != nil {
		status = StatusFailed
		output = err.Error()
	} else if result != nil {
		output = result.Output
	}

	_ = d.db.Model(proj).Update("deploy_status", status).Error
	d.recordHistory(proj, output, status)

	return result, err
}

// GenerateWebhookSecret produces a random hex secret for webhook authentication.
func GenerateWebhookSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (d *Deployer) deployImage(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	image := cfg.Image
	if image == "" {
		image = proj.Source
	}
	tag := cfg.Tag
	if tag == "" {
		tag = "latest"
	}
	fullImage := fmt.Sprintf("%s:%s", image, tag)

	name := sanitizeName(proj.Name)
	envFlags := buildEnvFlags(cfg.EnvVars)
	portFlags := buildPortFlags(cfg.Ports)
	volumeFlags := buildVolumeFlags(cfg.Volumes)

	res, err := d.exec.Run(fmt.Sprintf(
		"docker pull %s 2>&1 && docker rm -f %s 2>/dev/null; docker run -d --name %s --restart unless-stopped %s %s %s %s 2>&1",
		fullImage, name, name, envFlags, portFlags, volumeFlags, fullImage,
	))
	if err != nil {
		return nil, fmt.Errorf("deploy image: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("deploy image: %s", res.Stdout+res.Stderr)
	}
	return &DeployResult{Status: StatusRunning, Output: res.Stdout}, nil
}

func (d *Deployer) deployCompose(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	dir := fmt.Sprintf("/opt/xmanager/projects/%s", sanitizeName(proj.Name))
	yaml := cfg.ComposeYAML
	if yaml == "" {
		yaml = proj.Source
	}

	if err := d.writeRemoteFile(dir+"/docker-compose.yml", yaml); err != nil {
		return nil, fmt.Errorf("writing compose file: %w", err)
	}

	res, err := d.exec.Run(fmt.Sprintf("cd %s && docker compose up -d 2>&1", dir))
	if err != nil {
		return nil, fmt.Errorf("compose up: %w", err)
	}
	return &DeployResult{Status: StatusRunning, Output: res.Stdout + res.Stderr}, nil
}

func (d *Deployer) deployDockerfile(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	name := sanitizeName(proj.Name)
	dir := fmt.Sprintf("/opt/xmanager/projects/%s", name)
	dockerfile := cfg.DockerfileContent
	if dockerfile == "" {
		dockerfile = proj.Source
	}

	if err := d.writeRemoteFile(dir+"/Dockerfile", dockerfile); err != nil {
		return nil, fmt.Errorf("writing Dockerfile: %w", err)
	}

	buildOut := d.exec.RunQuiet(fmt.Sprintf("cd %s && docker build -t %s . 2>&1", dir, name))

	portFlags := buildPortFlags(cfg.Ports)
	envFlags := buildEnvFlags(cfg.EnvVars)
	res, err := d.exec.Run(fmt.Sprintf(
		"docker rm -f %s 2>/dev/null; docker run -d --name %s --restart unless-stopped %s %s %s 2>&1",
		name, name, envFlags, portFlags, name,
	))
	if err != nil {
		return nil, fmt.Errorf("run container: %w", err)
	}
	return &DeployResult{Status: StatusRunning, Output: buildOut + "\n" + res.Stdout}, nil
}

func (d *Deployer) deployGit(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	name := sanitizeName(proj.Name)
	dir := fmt.Sprintf("/opt/xmanager/projects/%s", name)
	repoURL := cfg.RepoURL
	if repoURL == "" {
		repoURL = proj.Source
	}
	branch := cfg.Branch
	if branch == "" {
		branch = "main"
	}

	cloneOrPull := fmt.Sprintf(
		"if [ -d %s/.git ]; then cd %s && git fetch origin && git reset --hard origin/%s; else git clone --depth 1 -b %s %s %s; fi 2>&1",
		dir, dir, branch, branch, repoURL, dir,
	)
	res, err := d.exec.Run(cloneOrPull)
	if err != nil {
		return nil, fmt.Errorf("git clone/pull: %w", err)
	}

	// try docker compose first, fall back to Dockerfile
	composeCheck := d.exec.RunQuiet(fmt.Sprintf("test -f %s/docker-compose.yml && echo yes", dir))
	if composeCheck == "yes" {
		upRes, err := d.exec.Run(fmt.Sprintf("cd %s && docker compose up -d 2>&1", dir))
		if err != nil {
			return nil, err
		}
		return &DeployResult{Status: StatusRunning, Output: res.Stdout + upRes.Stdout}, nil
	}

	buildRes, err := d.exec.Run(fmt.Sprintf("cd %s && docker build -t %s . 2>&1", dir, name))
	if err != nil {
		return nil, fmt.Errorf("docker build: %w", err)
	}
	runRes, _ := d.exec.Run(fmt.Sprintf(
		"docker rm -f %s 2>/dev/null; docker run -d --name %s --restart unless-stopped %s 2>&1",
		name, name, name,
	))
	return &DeployResult{Status: StatusRunning, Output: res.Stdout + buildRes.Stdout + runRes.Stdout}, nil
}

func (d *Deployer) deployOneClick(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	// ComposeYAML is rendered by apps/loader before being stored in cfg
	return d.deployCompose(proj, cfg)
}

func (d *Deployer) deployClone(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	// Clone = git clone without build; useful for config repos
	name := sanitizeName(proj.Name)
	dir := fmt.Sprintf("/opt/xmanager/projects/%s", name)
	repoURL := cfg.RepoURL
	if repoURL == "" {
		repoURL = proj.Source
	}
	branch := cfg.Branch
	if branch == "" {
		branch = "main"
	}
	cmd := fmt.Sprintf(
		"rm -rf %s && git clone --depth 1 -b %s %s %s 2>&1",
		dir, branch, repoURL, dir,
	)
	res, err := d.exec.Run(cmd)
	if err != nil {
		return nil, fmt.Errorf("clone: %w", err)
	}
	return &DeployResult{Status: StatusRunning, Output: res.Stdout}, nil
}

func (d *Deployer) deployPush(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	// Image was pre-pushed by the caller; just run it.
	image := cfg.PushedImage
	if image == "" {
		image = proj.Source
	}
	cfg.Image = image
	cfg.Tag = ""
	return d.deployImage(proj, cfg)
}

func (d *Deployer) deployScratch(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	name := sanitizeName(proj.Name)
	dir := fmt.Sprintf("/opt/xmanager/projects/%s", name)
	_, err := d.exec.Run(fmt.Sprintf("mkdir -p %s", dir))
	if err != nil {
		return nil, fmt.Errorf("creating scratch dir: %w", err)
	}
	return &DeployResult{Status: StatusStopped, Output: "scratch dir created: " + dir}, nil
}

func (d *Deployer) deployArchive(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	name := sanitizeName(proj.Name)
	dir := fmt.Sprintf("/opt/xmanager/projects/%s", name)
	archivePath := cfg.ArchivePath
	if archivePath == "" {
		archivePath = proj.Source
	}

	res, err := d.exec.Run(fmt.Sprintf(
		"mkdir -p %s && tar -xzf %s -C %s 2>&1",
		dir, archivePath, dir,
	))
	if err != nil {
		return nil, fmt.Errorf("extracting archive: %w", err)
	}
	return &DeployResult{Status: StatusRunning, Output: res.Stdout + res.Stderr}, nil
}

func (d *Deployer) deployFunction(proj *storage.Project, cfg *Config) (*DeployResult, error) {
	name := sanitizeName(proj.Name)
	runtime := cfg.Runtime
	if runtime == "" {
		runtime = "python3"
	}

	image := runtimeImage(runtime)
	dir := fmt.Sprintf("/opt/xmanager/functions/%s", name)

	if err := d.writeRemoteFile(dir+"/handler.py", proj.Source); err != nil {
		return nil, fmt.Errorf("writing function: %w", err)
	}

	portFlags := buildPortFlags(cfg.Ports)
	res, err := d.exec.Run(fmt.Sprintf(
		"docker rm -f fn-%s 2>/dev/null; docker run -d --name fn-%s --restart unless-stopped -v %s:/app %s %s python3 /app/handler.py 2>&1",
		name, name, dir, portFlags, image,
	))
	if err != nil {
		return nil, fmt.Errorf("deploy function: %w", err)
	}
	return &DeployResult{Status: StatusRunning, Output: res.Stdout + res.Stderr}, nil
}

func (d *Deployer) writeRemoteFile(path, content string) error {
	dir := path[:strings.LastIndex(path, "/")]
	_, _ = d.exec.Run(fmt.Sprintf("mkdir -p %s", dir))
	cmd := fmt.Sprintf("cat > %s << 'XEOF'\n%s\nXEOF", path, content)
	res, err := d.exec.Run(cmd)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("write failed (exit %d): %s", res.ExitCode, res.Stderr)
	}
	return nil
}

func (d *Deployer) recordHistory(proj *storage.Project, output, status string) {
	history := &storage.DeployHistory{
		ServerID:    proj.ServerID,
		ProjectID:   proj.ID,
		Service:     proj.Name,
		Method:      proj.Type,
		Status:      status,
		Output:      output,
		TriggeredAt: time.Now(),
	}
	_ = d.db.Create(history).Error
}

func buildEnvFlags(envVars map[string]string) string {
	var parts []string
	for k, v := range envVars {
		parts = append(parts, fmt.Sprintf("-e %s=%s", k, shellQuote(v)))
	}
	return strings.Join(parts, " ")
}

func buildPortFlags(ports []string) string {
	var parts []string
	for _, p := range ports {
		parts = append(parts, "-p "+p)
	}
	return strings.Join(parts, " ")
}

func buildVolumeFlags(volumes []string) string {
	var parts []string
	for _, v := range volumes {
		parts = append(parts, "-v "+v)
	}
	return strings.Join(parts, " ")
}

func runtimeImage(runtime string) string {
	switch strings.ToLower(runtime) {
	case "python", "python3":
		return "python:3.12-slim"
	case "node", "nodejs":
		return "node:20-slim"
	case "ruby":
		return "ruby:3.3-slim"
	default:
		return "python:3.12-slim"
	}
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}
