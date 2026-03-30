package ssh

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type ClientConfig struct {
	Host       string
	Port       int
	User       string
	KeyPath    string
	Password   string
	JumpHost   string
	Timeout    time.Duration
}

type Client struct {
	config ClientConfig
	conn   *ssh.Client
}

func NewClient(cfg ClientConfig) *Client {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &Client{config: cfg}
}

func (c *Client) Connect() error {
	sshConfig, err := c.buildSSHConfig()
	if err != nil {
		return fmt.Errorf("building SSH config: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	if c.config.JumpHost != "" {
		return c.connectViaJump(sshConfig, addr)
	}

	conn, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	c.conn = conn
	return nil
}

func (c *Client) connectViaJump(sshConfig *ssh.ClientConfig, targetAddr string) error {
	jumpConn, err := ssh.Dial("tcp", c.config.JumpHost, sshConfig)
	if err != nil {
		return fmt.Errorf("connecting to jump host %s: %w", c.config.JumpHost, err)
	}

	netConn, err := jumpConn.Dial("tcp", targetAddr)
	if err != nil {
		jumpConn.Close()
		return fmt.Errorf("dialing target via jump: %w", err)
	}

	ncc, chans, reqs, err := ssh.NewClientConn(netConn, targetAddr, sshConfig)
	if err != nil {
		netConn.Close()
		jumpConn.Close()
		return fmt.Errorf("establishing SSH through jump: %w", err)
	}

	c.conn = ssh.NewClient(ncc, chans, reqs)
	return nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) IsConnected() bool {
	if c.conn == nil {
		return false
	}
	_, _, err := c.conn.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

func (c *Client) Underlying() *ssh.Client {
	return c.conn
}

func (c *Client) buildSSHConfig() (*ssh.ClientConfig, error) {
	var authMethods []ssh.AuthMethod

	if c.config.KeyPath != "" {
		key, err := loadPrivateKey(c.config.KeyPath)
		if err != nil {
			return nil, err
		}
		authMethods = append(authMethods, ssh.PublicKeys(key))
	}

	if agentAuth := sshAgentAuth(); agentAuth != nil {
		authMethods = append(authMethods, agentAuth)
	}

	if c.config.Password != "" {
		authMethods = append(authMethods, ssh.Password(c.config.Password))
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no authentication method available")
	}

	hostKeyCallback, err := knownHostsCallback()
	if err != nil {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	return &ssh.ClientConfig{
		User:            c.config.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         c.config.Timeout,
	}, nil
}

func loadPrivateKey(path string) (ssh.Signer, error) {
	if len(path) >= 2 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}

	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading SSH key %s: %w", path, err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH key %s: %w", path, err)
	}
	return signer, nil
}

func sshAgentAuth() ssh.AuthMethod {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	return ssh.PublicKeysCallback(agent.NewClient(conn).Signers)
}

func knownHostsCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	khPath := filepath.Join(home, ".ssh", "known_hosts")
	return knownhosts.New(khPath)
}
