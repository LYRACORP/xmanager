package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/apps"
	"github.com/lyracorp/xmanager/internal/auth"
	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/gitforge"
	"github.com/lyracorp/xmanager/internal/hostfirewall"
	"github.com/lyracorp/xmanager/internal/nodemetrics"
	"github.com/lyracorp/xmanager/internal/poller"
	"github.com/lyracorp/xmanager/internal/project"
	"github.com/lyracorp/xmanager/internal/proxy"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

type handler struct {
	opts        Options
	tmpl        *template.Template
	sess        *sessionStore
	staticFS    fs.FS
	nodeMode    bool
	node        *nodemetrics.Collector
	exec        *ssh.Executor
	localSrvID  uint
	oauthStates  *oauthStateStore
	deviceStates *deviceStateStore
	relayStates  *relayStateStore
}

// serverCardData holds display-ready data for a single server card.
type serverCardData struct {
	Server   storage.Server
	Snapshot storage.ServerMetricSnapshot
	LastSeen string
}

type nodeDBView struct {
	Type      string
	Databases []dbmanager.Database
	Users     []dbmanager.DBUser
}

// pageData is the common template context passed to all pages.
type pageData struct {
	Title          string
	Flash          string
	Session        *session
	NodeMode       bool
	ActiveNav      string
	ServerID       uint
	ServerCards    []serverCardData
	ServerCard     serverCardData
	Projects       []storage.Project
	ProjectTypes   []string
	AllServers     []storage.Server
	Monitors       []storage.UptimeMonitor
	Config         interface{}
	Node           nodemetrics.Snapshot
	NetRx          string
	NetTx          string
	UptimeHuman    string
	Containers     []docker.Container
	ContainerID    string
	LogText        string
	CronJobs       []storage.CronJob
	CronRuns       []storage.CronRun
	CronJobID      uint
	NodeDBs        []nodeDBView
	ProjectDBs     []storage.ProjectDatabase
	NodeServices   []nodeServiceView
	ProjectDomains    []storage.ProjectDomain
	ConnectedDomains  []storage.ConnectedDomain
	Mailboxes         []storage.Mailbox
	VHosts            []proxy.VHost
	AlertChannels     []storage.AlertChannel
	MailAPIMode       string
	PowerDNSReady     bool
	MailAPIReady      bool
	// Home summary (node panel)
	StatProjects   int
	StatContainers int
	StatDomains    int
	StatMailboxes  int
	StatDatabases  int
	WelcomeName    string
	// Projects PaaS UX
	Project            *storage.Project
	ProjectConfig      project.Config
	ProjectDomainsList []storage.ProjectDomain
	ProjectEnvVars     []storage.ProjectEnvVar
	DeployHistory      []storage.DeployHistory
	ActiveTab          string
	CreateType         string
	TemplateSummaries  []apps.Summary
	TemplateApp        *apps.App
	TemplateQuery      string
	// Git OAuth / repo picker
	GitProviders     []gitProviderView
	GitRepos        []gitforge.Repo
	GitProvider      string
	GitCredID        uint
	SelectedProvider string
	PublicURL        string
	// Device flow UI
	DeviceID        string
	DeviceUserCode  string
	DeviceVerifyURL string
	DeviceProvider  string
	DeviceInterval  int
	// Token bootstrap (no OAuth client configured)
	BootstrapProvider string
	BootstrapTokenURL string
	BootstrapHint     string
	BootstrapNext     string
}

func (h *handler) register(mux *http.ServeMux) {
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(h.staticFS)))

	mux.HandleFunc("GET /login", h.getLogin)
	mux.HandleFunc("POST /login", h.postLogin)
	mux.HandleFunc("POST /logout", h.requireAuth(h.postLogout))

	mux.HandleFunc("GET /setup", h.getSetup)
	mux.HandleFunc("POST /setup", h.postSetup)

	if h.nodeMode {
		h.registerNode(mux)
		return
	}

	mux.HandleFunc("GET /{$}", h.requireAuth(h.getFleet))
	mux.HandleFunc("GET /api/servers/{id}/metrics", h.requireAuth(h.getServerMetrics))
	mux.HandleFunc("GET /servers/{id}", h.requireAuth(h.getServerDetail))

	mux.HandleFunc("GET /projects", h.requireAuth(h.getProjects))
	mux.HandleFunc("POST /projects", h.requireAuth(h.postProjects))
	mux.HandleFunc("POST /projects/{id}/deploy", h.requireAuth(h.postProjectDeploy))

	mux.HandleFunc("GET /uptime", h.requireAuth(h.getUptime))
	mux.HandleFunc("POST /uptime", h.requireAuth(h.postUptime))

	mux.HandleFunc("GET /settings", h.requireAuth(h.getSettings))

	mux.HandleFunc("POST /webhook/{project_id}", h.postWebhook)
}

func (h *handler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// --- Auth handlers ---

func (h *handler) getLogin(w http.ResponseWriter, r *http.Request) {
	needsSetup, _ := auth.EnsureAdmin(h.opts.DB)
	if needsSetup {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	h.render(w, "login", pageData{Title: "Login", NodeMode: h.nodeMode})
}

func (h *handler) postLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	u, err := auth.Authenticate(h.opts.DB, username, password)
	if err != nil {
		h.render(w, "login", pageData{Title: "Login", NodeMode: h.nodeMode, Flash: "Invalid username or password."})
		return
	}

	token := h.sess.create(u.ID, u.Username, u.Role)
	setSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) postLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		h.sess.delete(c.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *handler) getSetup(w http.ResponseWriter, r *http.Request) {
	needsSetup, _ := auth.EnsureAdmin(h.opts.DB)
	if !needsSetup {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.render(w, "setup", pageData{Title: "Setup", NodeMode: h.nodeMode})
}

func (h *handler) postSetup(w http.ResponseWriter, r *http.Request) {
	needsSetup, _ := auth.EnsureAdmin(h.opts.DB)
	if !needsSetup {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" || password == "" {
		h.render(w, "setup", pageData{Title: "Setup", NodeMode: h.nodeMode, Flash: "Username and password are required."})
		return
	}
	if err := auth.CreateUser(h.opts.DB, username, password, "admin"); err != nil {
		h.render(w, "setup", pageData{Title: "Setup", NodeMode: h.nodeMode, Flash: "Failed to create user: " + err.Error()})
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Fleet handlers ---

func (h *handler) nodePage(sess *session, title string) pageData {
	snap := nodemetrics.Snapshot{}
	if h.node != nil {
		snap = h.node.Latest()
	}
	panelPort := h.opts.Config.Web.Port
	if panelPort == 0 {
		panelPort = 8080
	}
	for i := range snap.Ports {
		p := &snap.Ports[i]
		p.CanClose = p.Port > 0 && p.Port != 22 && p.Port != panelPort
	}
	return pageData{
		Title:       title,
		Session:     sess,
		NodeMode:    true,
		Node:        snap,
		NetRx:       nodemetrics.FormatBytes(snap.NetRxBytes),
		NetTx:       nodemetrics.FormatBytes(snap.NetTxBytes),
		UptimeHuman: nodemetrics.FormatUptime(snap.UptimeSec),
	}
}

func (h *handler) renderNodeMetrics(w http.ResponseWriter, sess *session, flash string) {
	data := h.nodePage(sess, "This Server")
	data.Flash = flash
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "node_metrics", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *handler) getNodeHome(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.nodePage(sess, "Home")
	data.ActiveNav = "home"
	if sess != nil {
		data.WelcomeName = sess.Username
	}
	sid := h.localServerID()
	var n int64
	h.opts.DB.Model(&storage.Project{}).Where("server_id = ?", sid).Count(&n)
	data.StatProjects = int(n)
	h.opts.DB.Model(&storage.ConnectedDomain{}).Where("server_id = ?", sid).Count(&n)
	data.StatDomains = int(n)
	h.opts.DB.Model(&storage.Mailbox{}).Where("server_id = ?", sid).Count(&n)
	data.StatMailboxes = int(n)
	h.opts.DB.Model(&storage.ProjectDatabase{}).Where("server_id = ?", sid).Count(&n)
	data.StatDatabases = int(n)
	data.StatContainers = data.Node.ContainerCount
	h.render(w, "node_dashboard", data)
}

func (h *handler) getNodeMetricsFragment(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	h.renderNodeMetrics(w, sess, "")
}

func (h *handler) panelProtectedPorts() map[int]string {
	port := h.opts.Config.Web.Port
	if port == 0 {
		port = 8080
	}
	return map[int]string{port: "web panel"}
}

func (h *handler) postNodePortsClose(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	if err := r.ParseForm(); err != nil {
		h.renderNodeMetrics(w, sess, "Bad request.")
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(r.FormValue("port")))
	if err != nil || port < 1 {
		h.renderNodeMetrics(w, sess, "Invalid port.")
		return
	}
	proto := r.FormValue("proto")
	if err := hostfirewall.Deny(port, proto, h.panelProtectedPorts()); err != nil {
		h.renderNodeMetrics(w, sess, "Close failed: "+err.Error())
		return
	}
	if h.node != nil {
		h.node.Refresh()
	}
	h.renderNodeMetrics(w, sess, fmt.Sprintf("Closed inbound port %d (firewall). Process may still listen locally.", port))
}

func (h *handler) postNodePortsOpen(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	if err := r.ParseForm(); err != nil {
		h.renderNodeMetrics(w, sess, "Bad request.")
		return
	}
	ports, proto, err := hostfirewall.ParsePortsList(r.FormValue("ports"))
	if err != nil {
		h.renderNodeMetrics(w, sess, "Open failed: "+err.Error())
		return
	}
	if err := hostfirewall.Allow(ports, proto); err != nil {
		h.renderNodeMetrics(w, sess, "Open failed: "+err.Error())
		return
	}
	if h.node != nil {
		h.node.Refresh()
	}
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = strconv.Itoa(p)
	}
	h.renderNodeMetrics(w, sess, fmt.Sprintf("Opened inbound port(s) %s/%s in firewall.", strings.Join(parts, ", "), proto))
}

func (h *handler) getFleet(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	var servers []storage.Server
	h.opts.DB.Order("name asc").Find(&servers)

	snaps, _ := poller.LatestSnapshots(h.opts.DB)
	cards := buildServerCards(servers, snaps)

	h.render(w, "fleet", pageData{
		Title:       "Fleet Overview",
		Session:     sess,
		NodeMode:    false,
		ServerCards: cards,
	})
}

func (h *handler) getServerMetrics(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var srv storage.Server
	if err := h.opts.DB.First(&srv, id).Error; err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var snap storage.ServerMetricSnapshot
	h.opts.DB.Where("server_id = ?", id).Order("sampled_at desc").First(&snap)

	card := serverCardData{
		Server:   srv,
		Snapshot: snap,
		LastSeen: formatLastSeen(srv.LastSeen),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "server_card", card); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *handler) getServerDetail(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var srv storage.Server
	if err := h.opts.DB.First(&srv, id).Error; err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	var snap storage.ServerMetricSnapshot
	h.opts.DB.Where("server_id = ?", id).Order("sampled_at desc").First(&snap)

	card := serverCardData{
		Server:   srv,
		Snapshot: snap,
		LastSeen: formatLastSeen(srv.LastSeen),
	}

	h.render(w, "server_detail", pageData{
		Title:      fmt.Sprintf("%s — Detail", srv.Name),
		Session:    sess,
		ServerCard: card,
	})
}

// --- Project handlers ---

func (h *handler) getProjects(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	var projects []storage.Project
	h.opts.DB.Preload("Server").Order("name asc").Find(&projects)

	var servers []storage.Server
	h.opts.DB.Order("name asc").Find(&servers)

	h.render(w, "projects", pageData{
		Title:      "Projects",
		Session:    sess,
		Projects:   projects,
		AllServers: servers,
	})
}

func (h *handler) postProjects(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	serverIDStr := r.FormValue("server_id")
	serverID, _ := strconv.ParseUint(serverIDStr, 10, 64)

	project := storage.Project{
		Name:     r.FormValue("name"),
		ServerID: uint(serverID),
		Type:     r.FormValue("type"),
		Source:   r.FormValue("source"),
		Domain:   r.FormValue("domain"),
	}
	h.opts.DB.Create(&project)
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

func (h *handler) postProjectDeploy(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var project storage.Project
	if err := h.opts.DB.First(&project, id).Error; err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	h.opts.DB.Model(&project).Update("deploy_status", "deploying")
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

// --- Uptime handlers ---

func (h *handler) getUptime(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	var monitors []storage.UptimeMonitor
	h.opts.DB.Order("name asc").Find(&monitors)

	h.render(w, "uptime", pageData{
		Title:    "Uptime",
		Session:  sess,
		Monitors: monitors,
	})
}

func (h *handler) postUptime(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	intervalSec, _ := strconv.Atoi(r.FormValue("interval_sec"))
	if intervalSec <= 0 {
		intervalSec = 60
	}
	monitor := storage.UptimeMonitor{
		Name:        r.FormValue("name"),
		URL:         r.FormValue("url"),
		IntervalSec: intervalSec,
		Enabled:     true,
	}
	h.opts.DB.Create(&monitor)
	http.Redirect(w, r, "/uptime", http.StatusSeeOther)
}

// --- Settings handler ---

func (h *handler) getSettings(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := pageData{
		Title:     "Settings",
		Session:   sess,
		NodeMode:  h.nodeMode,
		ActiveNav: "settings",
		Config:    h.opts.Config,
	}
	if h.nodeMode {
		data.GitProviders = h.gitProviderViews()
		if h.opts.Config != nil {
			data.PublicURL = h.opts.Config.Web.PublicURL
		}
		if flash := r.URL.Query().Get("flash"); flash != "" {
			data.Flash = flash
		}
	}
	h.render(w, "settings", data)
}

// --- Webhook handler (no auth, validates secret header) ---

func (h *handler) postWebhook(w http.ResponseWriter, r *http.Request) {
	projectIDStr := r.PathValue("project_id")
	projectID, err := strconv.ParseUint(projectIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return
	}

	var project storage.Project
	if err := h.opts.DB.First(&project, projectID).Error; err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	if project.WebhookSecret != "" {
		secret := r.Header.Get("X-Webhook-Secret")
		if secret != project.WebhookSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	h.opts.DB.Model(&project).Update("deploy_status", "deploying")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

// --- Helpers ---

func buildServerCards(servers []storage.Server, snaps map[uint]storage.ServerMetricSnapshot) []serverCardData {
	cards := make([]serverCardData, 0, len(servers))
	for _, s := range servers {
		cards = append(cards, serverCardData{
			Server:   s,
			Snapshot: snaps[s.ID],
			LastSeen: formatLastSeen(s.LastSeen),
		})
	}
	return cards
}

func formatLastSeen(t *time.Time) string {
	if t == nil {
		return "never"
	}
	d := time.Since(*t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
