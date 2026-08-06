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

// knownHostsCallback verifies host keys against ~/.ssh/known_hosts.
// Unknown hosts are accepted on first connect (TOFU) and appended to the file,
// matching typical OpenSSH interactive behavior. Changed keys still fail.
func knownHostsCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	khPath := filepath.Join(home, ".ssh", "known_hosts")

	// Ensure ~/.ssh and known_hosts exist so knownhosts.New succeeds.
	if err := os.MkdirAll(filepath.Dir(khPath), 0700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(khPath); os.IsNotExist(err) {
		if f, createErr := os.OpenFile(khPath, os.O_CREATE|os.O_WRONLY, 0600); createErr == nil {
			_ = f.Close()
		}
	}

	base, err := knownhosts.New(khPath)
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if !asKeyError(err, &keyErr) {
			return err
		}

		// Known host but key mismatch — do not silently trust.
		if len(keyErr.Want) > 0 {
			return fmt.Errorf("host key mismatch for %s (remote key changed; remove the old entry from %s)", hostname, khPath)
		}

		// Unknown host — trust on first use and persist.
		if appendErr := appendKnownHost(khPath, hostname, remote, key); appendErr != nil {
			return fmt.Errorf("accepting new host key: %w", appendErr)
		}
		return nil
	}, nil
}

func asKeyError(err error, target **knownhosts.KeyError) bool {
	ke, ok := err.(*knownhosts.KeyError)
	if !ok {
		return false
	}
	*target = ke
	return true
}

func appendKnownHost(khPath, hostname string, remote net.Addr, key ssh.PublicKey) error {
	f, err := os.OpenFile(khPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	// knownhosts.Line formats "[host]:port keytype base64..." correctly.
	addr := []string{hostname}
	if remote != nil {
		if host, _, splitErr := net.SplitHostPort(remote.String()); splitErr == nil && host != "" {
			addr = []string{knownhosts.Normalize(remote.String())}
		}
	}
	line := knownhosts.Line(addr, key)
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}
	return nil
}
