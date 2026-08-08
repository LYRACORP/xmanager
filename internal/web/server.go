package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/nodemetrics"
	"github.com/lyracorp/xmanager/internal/poller"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

//go:embed all:templates all:static
var assets embed.FS

// Options holds dependencies for the web server.
type Options struct {
	Config *config.Config
	DB     *gorm.DB
	Pool   *ssh.Pool
	Poller *poller.Poller
}

// Run starts the HTTP server and blocks until it exits.
func Run(opts Options) error {
	webCfg := opts.Config.Web
	host := webCfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := webCfg.Port
	if port == 0 {
		port = 8080
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	funcMap := template.FuncMap{
		"formatBytes":  nodemetrics.FormatBytes,
		"formatUptime": nodemetrics.FormatUptime,
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(assets, "templates/*.html")
	if err != nil {
		return fmt.Errorf("parsing templates: %w", err)
	}

	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return fmt.Errorf("static sub-fs: %w", err)
	}

	h := &handler{
		opts:     opts,
		tmpl:     tmpl,
		sess:     newSessionStore(),
		staticFS: staticFS,
		nodeMode: opts.Config.Web.IsNode(),
	}

	if h.nodeMode {
		h.exec = ssh.NewLocalExecutor()
		if srv, err := EnsureLocalServer(opts.DB); err == nil {
			h.localSrvID = srv.ID
		}
		h.node = nodemetrics.NewCollector(5 * time.Second)
		h.node.Start()
		defer h.node.Stop()
	}

	mux := http.NewServeMux()
	h.register(mux)

	fmt.Printf("XManager web listening on http://%s (role=%s)\n", addr, opts.Config.Web.Role)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return srv.ListenAndServe()
}
