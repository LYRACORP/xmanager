package proxy

import "github.com/lyracorp/xmanager/internal/ssh"

type ProxyType string

const (
	Nginx   ProxyType = "nginx"
	Traefik ProxyType = "traefik"
)

type VHost struct {
	Domain    string
	Upstream  string
	SSLEnabled bool
	SSLExpiry  string
	ConfigFile string
}

type TraefikRouter struct {
	Name    string
	Rule    string
	Service string
	TLS     bool
}

type Manager interface {
	Type() ProxyType
	IsAvailable() bool
	ListVHosts() ([]VHost, error)
	ValidateConfig() (string, error)
	ReloadConfig() error
}

func NewManager(proxyType ProxyType, exec *ssh.Executor) Manager {
	switch proxyType {
	case Nginx:
		return &NginxManager{exec: exec}
	case Traefik:
		return &TraefikManager{exec: exec}
	default:
		return nil
	}
}
