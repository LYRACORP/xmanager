package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/lyracorp/xmanager/internal/storage"
)

var containerIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

var termUpgrader = websocket.Upgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type termResizeMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func (h *handler) requireAuthWS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sess, ok := h.sess.get(c.Value)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(withSession(r.Context(), sess)))
	}
}

func sanitizeContainerRef(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || !containerIDRe.MatchString(id) || len(id) > 128 {
		return "", fmt.Errorf("invalid container id")
	}
	return id, nil
}

func containerRunning(nameOrID string) bool {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", nameOrID).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// detectContainerShell finds a usable shell inside the container.
func detectContainerShell(container string) (string, error) {
	out, err := exec.Command("docker", "exec", container, "sh", "-c",
		`command -v bash 2>/dev/null || command -v ash 2>/dev/null || command -v sh 2>/dev/null || ls /bin/bash /bin/sh /bin/ash 2>/dev/null | head -1`).Output()
	shell := strings.TrimSpace(string(out))
	if err != nil || shell == "" {
		return "", fmt.Errorf("no shell in container %q (distroless/scratch images cannot open a terminal)", container)
	}
	// First line only.
	if i := strings.IndexByte(shell, '\n'); i >= 0 {
		shell = strings.TrimSpace(shell[:i])
	}
	return shell, nil
}

func (h *handler) getNodeDockerTerminal(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	id, err := sanitizeContainerRef(r.PathValue("id"))
	if err != nil {
		http.Redirect(w, r, "/docker?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	data := h.basePage(sess, "Terminal")
	data.ActiveNav = "docker"
	data.ContainerID = id
	if !containerRunning(id) {
		data.Flash = "Container is not running"
	}
	h.render(w, "node_docker_terminal", data)
}

func (h *handler) getNodeDockerTerminalWS(w http.ResponseWriter, r *http.Request) {
	id, err := sanitizeContainerRef(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.serveContainerTerminalWS(w, r, id)
}

func (h *handler) getNodeProjectTerminalWS(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid project", http.StatusBadRequest)
		return
	}
	p, err := h.loadNodeProject(uint(pid))
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	container, err := resolveProjectContainer(p, r.URL.Query().Get("container"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	h.serveContainerTerminalWS(w, r, container)
}

func wsWrite(conn *websocket.Conn, mu *sync.Mutex, data []byte) error {
	mu.Lock()
	defer mu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func (h *handler) serveContainerTerminalWS(w http.ResponseWriter, r *http.Request, container string) {
	if !containerRunning(container) {
		http.Error(w, "container not running", http.StatusConflict)
		return
	}

	shell, shellErr := detectContainerShell(container)

	conn, err := termUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var writeMu sync.Mutex
	writeNote := func(s string) {
		_ = wsWrite(conn, &writeMu, []byte(s))
	}

	if shellErr != nil {
		writeNote("\r\n\x1b[31m" + shellErr.Error() + "\x1b[0m\r\n")
		time.Sleep(50 * time.Millisecond)
		return
	}

	// -i keeps stdin open; -t allocates a TTY inside the container.
	// Outer creack/pty provides the host-side TTY docker expects for -t.
	cmd := exec.Command("docker", "exec", "-i", "-t", container, shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		writeNote("\r\n\x1b[31mfailed to start shell: " + err.Error() + "\x1b[0m\r\n")
		return
	}

	var once sync.Once
	var doneOnce sync.Once
	done := make(chan struct{})
	closeDone := func() { doneOnce.Do(func() { close(done) }) }
	closeAll := func() {
		once.Do(func() {
			closeDone()
			_ = ptmx.Close()
			_ = conn.Close()
		})
	}
	defer closeAll()

	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 30, Cols: 120})

	// Exit watcher — surface docker exec failures in the terminal.
	go func() {
		err := cmd.Wait()
		msg := "\r\n\x1b[90mshell exited\x1b[0m\r\n"
		if err != nil {
			msg = "\r\n\x1b[31mshell exited: " + err.Error() + "\x1b[0m\r\n"
		}
		_ = wsWrite(conn, &writeMu, []byte(msg))
		time.Sleep(100 * time.Millisecond)
		closeAll()
	}()

	// Keepalive pings so proxies don't drop the socket.
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				writeMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				err := conn.WriteMessage(websocket.PingMessage, []byte("ping"))
				writeMu.Unlock()
				if err != nil {
					closeAll()
					return
				}
			case <-done:
				return
			}
		}
	}()

	// PTY → WebSocket
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if werr := wsWrite(conn, &writeMu, buf[:n]); werr != nil {
					closeAll()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Time{})
	conn.SetPongHandler(func(string) error { return nil })

	// WebSocket → PTY
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			closeAll()
			return
		}
		if mt == websocket.TextMessage && len(data) > 0 && data[0] == '{' {
			var msg termResizeMsg
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Rows: msg.Rows, Cols: msg.Cols})
				continue
			}
		}
		if _, err := ptmx.Write(data); err != nil {
			closeAll()
			return
		}
	}
}

func projectContainerCandidates(p *storage.Project) []string {
	slug := projectSlug(p.Name)
	if strings.EqualFold(p.Type, "function") {
		return []string{"fn-" + slug}
	}
	out := []string{slug}
	raw, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		return out
	}
	seen := map[string]bool{slug: true}
	for _, name := range strings.Split(string(raw), "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, slug+"-") || strings.HasPrefix(lower, slug+"_") || lower == slug {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

func resolveProjectContainer(p *storage.Project, prefer string) (string, error) {
	prefer = strings.TrimSpace(prefer)
	if prefer != "" {
		id, err := sanitizeContainerRef(prefer)
		if err != nil {
			return "", err
		}
		if containerRunning(id) {
			return id, nil
		}
		return "", fmt.Errorf("container %s is not running", id)
	}
	for _, c := range projectContainerCandidates(p) {
		if containerRunning(c) {
			return c, nil
		}
	}
	return "", fmt.Errorf("no running container found for this project")
}
