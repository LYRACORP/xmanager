package project

// Type represents the deployment method for a project.
type Type string

const (
	TypeImage      Type = "image"
	TypeCompose    Type = "compose"
	TypeDockerfile Type = "dockerfile"
	TypeGit        Type = "git"
	TypeOneClick   Type = "oneclick"
	TypeClone      Type = "clone"
	TypePush       Type = "push"
	TypeScratch    Type = "scratch"
	TypeArchive    Type = "archive"
	TypeFunction   Type = "function"
)

// DeployStatus values.
const (
	StatusPending  = "pending"
	StatusBuilding = "building"
	StatusRunning  = "running"
	StatusStopped  = "stopped"
	StatusFailed   = "failed"
)

// Config carries deployment parameters, stored as JSON in Project.ConfigJSON.
type Config struct {
	// Image type
	Image string `json:"image,omitempty"`
	Tag   string `json:"tag,omitempty"`

	// Git / Clone type
	RepoURL  string `json:"repo_url,omitempty"`
	Branch   string `json:"branch,omitempty"`
	CredID   uint   `json:"cred_id,omitempty"`

	// Compose / OneClick
	ComposeYAML string `json:"compose_yaml,omitempty"`

	// Dockerfile
	DockerfileContent string `json:"dockerfile_content,omitempty"`
	BuildContext      string `json:"build_context,omitempty"`

	// Push — receive a pre-built image tag
	PushedImage string `json:"pushed_image,omitempty"`

	// Archive — tar.gz path on remote
	ArchivePath string `json:"archive_path,omitempty"`

	// Function
	Runtime string `json:"runtime,omitempty"`
	Handler string `json:"handler,omitempty"`

	// Common
	EnvVars  map[string]string `json:"env_vars,omitempty"`
	Ports    []string          `json:"ports,omitempty"`
	Volumes  []string          `json:"volumes,omitempty"`
	Networks []string          `json:"networks,omitempty"`
}

// DeployResult is the outcome of a deployment attempt.
type DeployResult struct {
	Status  string
	Output  string
	ImageID string
}
