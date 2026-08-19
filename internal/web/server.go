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

	"github.com/lyracorp/xmanager/internal/ai"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/nodemetrics"
	"github.com/lyracorp/xmanager/internal/ops"
	"github.com/lyracorp/xmanager/internal/poller"
	"github.com/lyracorp/xmanager/internal/reqdump"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/workflow"
	"gorm.io/gorm"
)

//go:embed all:templates all:static
var assets embed.FS

// Options holds dependencies for the web server.
type Options struct {
	Config    *config.Config
	DB        *gorm.DB
	Pool      *ssh.Pool
	Poller    *poller.Poller
	Catalog   *ops.Catalog
	Workflows *workflow.Engine
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

	funcMap := webTemplateFuncs()

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(assets, "templates/*.html")
	if err != nil {
		return fmt.Errorf("parsing templates: %w", err)
	}

	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return fmt.Errorf("static sub-fs: %w", err)
	}

	h := &handler{
		opts:         opts,
		tmpl:         tmpl,
		sess:         newSessionStore(),
		staticFS:     staticFS,
		nodeMode:     opts.Config.Web.IsNode(),
		oauthStates:  newOAuthStateStore(),
		deviceStates: newDeviceStateStore(),
		pending2FA:   newPending2FAStore(),
		totpEnroll:   newTOTPEnrollStore(),
		totpReveal:   newTOTPRevealStore(),
	}
	if opts.Catalog != nil {
		h.catalog = opts.Catalog
	} else {
		h.catalog = ops.New(opts.DB, opts.Pool)
	}
	if h.catalog != nil && h.catalog.Completer == nil {
		h.catalog.Completer = ai.Completer(opts.Config)
	}
	if opts.Workflows != nil {
		h.workflows = opts.Workflows
	} else {
		h.workflows = workflow.New(opts.DB, h.catalog)
	}
	wfSched := workflow.NewScheduler(h.workflows)
	wfSched.Start()
	defer wfSched.Stop()

	if h.nodeMode {
		h.exec = ssh.NewLocalExecutor()
		if srv, err := EnsureLocalServer(opts.DB); err == nil {
			h.localSrvID = srv.ID
		}
		panelPort := port
		h.dumpMgr = reqdump.NewManager(opts.DB, h.localSrvID, panelPort)
		h.secStack = newSecurityStack(opts.DB, h.localSrvID, h.dumpMgr)
		h.node = nodemetrics.NewCollector(5 * time.Second)
		h.node.Start()
		defer h.node.Stop()
		h.startChartSampler()
		defer h.stopChartSampler()
		h.ensureDefaultNodeServices()
		h.reloadSecurityAll()
		defer func() {
			h.dumpMgr.Stop()
			h.secStack.stop()
		}()
		h.secStack.startNginxPoller(h.exec)
	}

	mux := http.NewServeMux()
	h.register(mux)

	key, err := config.EnsureAccessKey(opts.Config)
	if err != nil {
		return fmt.Errorf("panel access key: %w", err)
	}

	inner := http.Handler(mux)
	if h.secStack != nil {
		inner = h.secStack.wrap(mux)
	} else if h.dumpMgr != nil {
		inner = h.dumpMgr.Middleware(mux)
	}
	h.access = newAccessGate(key, inner)

	fmt.Printf("XManager web listening on http://%s/%s/ (role=%s)\n", addr, key, opts.Config.Web.Role)
	// No WriteTimeout/ReadTimeout: WebSocket terminals are long-lived hijacked
	// connections; header timeout still bounds slowloris on new requests.
	srv := &http.Server{
		Addr:              addr,
		Handler:           h.access,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}
