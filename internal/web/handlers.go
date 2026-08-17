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

	"github.com/lyracorp/xmanager/internal/activity"
	"github.com/lyracorp/xmanager/internal/apps"
	"github.com/lyracorp/xmanager/internal/auth"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/gitforge"
	"github.com/lyracorp/xmanager/internal/hostfirewall"
	"github.com/lyracorp/xmanager/internal/nodemetrics"
	"github.com/lyracorp/xmanager/internal/poller"
	"github.com/lyracorp/xmanager/internal/project"
	"github.com/lyracorp/xmanager/internal/proxy"
	"github.com/lyracorp/xmanager/internal/reqdump"
	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/securityevents"
	"github.com/lyracorp/xmanager/internal/services/cloudflare"
	"github.com/lyracorp/xmanager/internal/services/mailinbox"
	"github.com/lyracorp/xmanager/internal/services/powerdns"
	"github.com/lyracorp/xmanager/internal/services/rustfs"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

type handler struct {
	opts         Options
	tmpl         *template.Template
	sess         *sessionStore
	staticFS     fs.FS
	nodeMode     bool
	node         *nodemetrics.Collector
	exec         *ssh.Executor
	localSrvID   uint
	oauthStates  *oauthStateStore
	deviceStates *deviceStateStore
	relayStates  *relayStateStore
	diskCache    *diskUsageCache
	chartStop    chan struct{}
	dumpMgr      *reqdump.Manager
	secStack     *securityStack
	access       *accessGate
}

// serverCardData holds display-ready data for a single server card.
type serverCardData struct {
	Server   storage.Server
	Snapshot storage.ServerMetricSnapshot
	LastSeen string
}

type nodeDBView struct {
	Type        string
	Container   string
	Image       string
	Ports       string
	Port        string
	Host        string
	ContainerID string
	CPUPct      string
	MemUsage    string
	Running     bool
	LogsURL     string
	AdminerURL  string
	AdminerOn   bool
	PgAdminURL  string
	PgAdminOn   bool
	Databases   []dbmanager.Database
	Users       []dbmanager.DBUser
}

type nodeDBToolView struct {
	Name      string
	Container string
	Port      string
	URL       string
	Running   bool
}

type nodeDBDetailView struct {
	Type         string
	Name         string
	Owner        string
	Size         string
	Connections  int
	Tables       int
	Engine       nodeDBView
	AdminerURL   string
	PgAdminURL   string
	PgAdminOn    bool
	EngineCPUPct string
	EngineMem    string
}

type projectFileEntry struct {
	Name      string
	RelPath   string
	Mode      string
	IsDir     bool
	Size      int64
	SizeHuman string
	Mod       string
}

type projectFileCrumb struct {
	Name string
	Path string
}

// pageData is the common template context passed to all pages.
type pageData struct {
	Title             string
	Flash             string
	Session           *session
	IsAdmin           bool
	UserRole          string
	NodeMode          bool
	ActiveNav         string
	ServerID          uint
	ServerCards       []serverCardData
	ServerCard        serverCardData
	Projects          []storage.Project
	ProjectViews      []projectCardView
	ProjectTypes      []string
	AllServers        []storage.Server
	Monitors          []storage.UptimeMonitor
	Config            interface{}
	Node              nodemetrics.Snapshot
	NetRx             string
	NetTx             string
	UptimeHuman       string
	Containers        []docker.Container
	DockerHost        docker.HostSnapshot
	Swarm             docker.SwarmInfo
	SwarmNodes        []docker.SwarmNode
	SwarmServices     []docker.SwarmService
	SwarmWorkerJoin   string
	SwarmManagerJoin  string
	ContainerID       string
	LogText           string
	CronJobs          []storage.CronJob
	CronJobViews      []cronJobView
	CronRuns          []storage.CronRun
	CronJobID         uint
	NodeDBs           []nodeDBView
	DBDetail          nodeDBDetailView
	DBTools           []nodeDBToolView
	DBAvailable       map[string]bool
	DBEngines         []string
	ProjectDBs        []storage.ProjectDatabase
	NodeServices      []nodeServiceView
	ProjectDomains    []storage.ProjectDomain
	ConnectedDomains  []storage.ConnectedDomain
	Domain            *storage.ConnectedDomain
	Mailboxes         []storage.Mailbox
	VHosts            []proxy.VHost
	AlertChannels     []storage.AlertChannel
	MailAPIMode       string
	PowerDNSReady     bool
	MailAPIReady      bool
	DNSZones          []powerdns.Zone
	DNSZone           *powerdns.Zone
	DNSZoneName       string
	CFRecords         []cloudflare.Record
	NodeSettings      *storage.NodeSettings
	MailDomains       []mailinbox.MailDomain
	MailDomainList    []string
	MailAPIConfig     mailinbox.Config // password cleared before render
	WebmailURL        string
	MailAdminURL      string
	WebmailHosts      []webmailHostView
	ServerWebmailHost string
	ServerWebmailURL  string
	SystemServices    []systemServiceView
	SystemSvcFilter   string
	FTPEnabled        bool
	FTPUsers          []storage.FTPUser
	FTPUser           *storage.FTPUser
	PanelUsers        []storage.User
	AccessKey         string
	PanelURL          string
	// Home summary (node panel)
	StatProjects   int
	StatContainers int
	StatDomains    int
	StatMailboxes  int
	StatDatabases  int
	StatBuckets    int
	WelcomeName    string
	DiskUsage      diskUsageView
	NetRxRate      string
	NetTxRate      string
	// Projects PaaS UX
	Project            *storage.Project
	ProjectConfig      project.Config
	ProjectDomainsList []storage.ProjectDomain
	ProjectEnvVars     []storage.ProjectEnvVar
	DeployHistory      []storage.DeployHistory
	ActiveTab          string
	CreateType         string
	FilePath           string
	FileEntries        []projectFileEntry
	FileEditPath       string
	FileEditBody       string
	FileRoot           string
	FileCrumbs         []projectFileCrumb
	FileParent         string
	FileScope          string // host | container
	FileContainer      string
	TermContainer      string
	TermContainers     []string
	TemplateSummaries  []apps.Summary
	TemplateApp        *apps.App
	TemplateQuery      string
	// Git OAuth / repo picker
	GitProviders     []gitProviderView
	GitRepos         []gitforge.Repo
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
	// Storage (RustFS)
	StorageReady        bool
	StorageEndpoint     string
	StorageConsoleURL   string
	StorageBuckets      []rustfs.BucketInfo
	StorageBucket       string
	StoragePrefix       string
	StorageParent       string
	StorageObjects      []rustfs.ObjectEntry
	StorageCrumbs       []projectFileCrumb
	StorageTotalObjects int64
	StorageTotalBytes   int64
	StorageTotalHuman   string
	// Backup
	BackupTargets          []backupTargetView
	BackupDestinations     []storage.BackupDestination
	BackupHistory          []backupHistoryView
	BackupSchedules        []backupScheduleView
	BackupTelegramChannels []storage.AlertChannel
	// Activity / system logs
	ActivityLogs   []storage.ActivityLog
	LogsSource     string
	LogsActor      string
	LogsQuery      string
	LogsRange      string
	LogsProject    string
	LogsContainer  string
	LogsLabel      string
	LogsTabSources []struct{ ID, Label string }
	LogsFragmentQS string
	// Security
	Security       securityPageView
	SecurityPolicy security.Policy
	ModsecStatus   wafModsecView
	TrafficRange   string
	RequestDump    *storage.RequestDump
	RequestDumpHex string
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
	mux.HandleFunc("POST /settings/password", h.requireAuth(h.postSettingsPassword))
	mux.HandleFunc("POST /settings/access-key", h.requireAdminAuth(h.postSettingsAccessKey))
	mux.HandleFunc("POST /settings/logs", h.requireAuth(h.postSettingsLogs))

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
	ip := clientIPFromRequest(r)
	if err != nil {
		securityevents.Log(h.opts.DB, securityevents.Entry{
			ServerID: h.localServerID(),
			Kind:     securityevents.KindLoginFail,
			IP:       ip,
			Actor:    username,
			Detail:   "invalid credentials",
		})
		h.render(w, "login", pageData{Title: "Login", NodeMode: h.nodeMode, Flash: "Invalid username or password."})
		return
	}

	securityevents.Log(h.opts.DB, securityevents.Entry{
		ServerID: h.localServerID(),
		Kind:     securityevents.KindLoginOK,
		IP:       ip,
		Actor:    u.Username,
	})

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
	data := pageData{
		Title:       title,
		Session:     sess,
		NodeMode:    true,
		Node:        snap,
		NetRx:       nodemetrics.FormatBytes(snap.NetRxBytes),
		NetTx:       nodemetrics.FormatBytes(snap.NetTxBytes),
		UptimeHuman: nodemetrics.FormatUptime(snap.UptimeSec),
	}
	fillPageACL(&data, sess)
	return data
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
	q := h.scopeServerQuery(r, &storage.Project{})
	var n int64
	q.Count(&n)
	data.StatProjects = int(n)
	h.scopeServerQuery(r, &storage.ConnectedDomain{}).Count(&n)
	data.StatDomains = int(n)
	h.scopeServerQuery(r, &storage.Mailbox{}).Count(&n)
	data.StatMailboxes = int(n)
	h.scopeServerQuery(r, &storage.ProjectDatabase{}).Count(&n)
	data.StatDatabases = int(n)
	data.StatContainers = data.Node.ContainerCount
	data.StatBuckets = h.countOwnedRustFSBuckets(r)
	data.DiskUsage = h.getDiskUsageCached()
	data.NetRxRate = nodemetrics.FormatRateKbps(data.Node.NetRxBps)
	data.NetTxRate = nodemetrics.FormatRateKbps(data.Node.NetTxBps)
	h.render(w, "node_dashboard", data)
}

func (h *handler) getNodeMetricsFragment(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	h.renderNodeMetrics(w, sess, "")
}

func (h *handler) getNodeDiskUsageFragment(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.nodePage(sess, "Home")
	data.DiskUsage = h.getDiskUsageCached()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "node_disk_usage", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *handler) getNodeNetJSON(w http.ResponseWriter, r *http.Request) {
	snap := nodemetrics.Snapshot{}
	if h.node != nil {
		snap = h.node.Latest()
	} else {
		snap = nodemetrics.Sample()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"iface":      snap.NetIface,
		"rx_bps":     snap.NetRxBps,
		"tx_bps":     snap.NetTxBps,
		"rx":         snap.NetRxBytes,
		"tx":         snap.NetTxBytes,
		"rx_rate":    nodemetrics.FormatRateKbps(snap.NetRxBps),
		"tx_rate":    nodemetrics.FormatRateKbps(snap.NetTxBps),
		"t":          snap.SampledAt.Unix(),
		"uptime":     nodemetrics.FormatUptime(snap.UptimeSec),
		"uptime_sec": snap.UptimeSec,
		"cpu_pct":    snap.CPUPct,
		"ram_pct":    snap.RAMPct,
		"disk_pct":   snap.DiskPct,
	})
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
	fillPageACL(&data, sess)
	if data.IsAdmin {
		data.AccessKey = h.currentAccessKey()
		if data.AccessKey != "" {
			data.PanelURL = strings.TrimRight(h.publicPanelURL(r), "/") + "/"
		}
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	if h.nodeMode {
		if data.IsAdmin {
			data.PanelUsers = h.loadPanelUsers()
		}
		data.GitProviders = h.gitProviderViews()
		if h.opts.Config != nil {
			data.PublicURL = h.opts.Config.Web.PublicURL
		}
		sid := h.localServerID()
		var ns storage.NodeSettings
		if err := h.opts.DB.Where("server_id = ?", sid).First(&ns).Error; err == nil {
			data.NodeSettings = &ns
		} else {
			data.NodeSettings = &storage.NodeSettings{ServerID: sid}
		}
	}
	h.render(w, "settings", data)
}

func (h *handler) postSettingsLogs(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	n, err := strconv.Atoi(strings.TrimSpace(r.FormValue("retention_days")))
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("retention days must be a number"), http.StatusSeeOther)
		return
	}
	if h.opts.Config == nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("config not loaded"), http.StatusSeeOther)
		return
	}
	h.opts.Config.Log.RetentionDays = config.ClampRetentionDays(n)
	flash := "Log retention saved"
	if err := config.Save(h.opts.Config); err != nil {
		flash = "Save failed: " + err.Error()
	} else {
		activity.SetRetentionDays(h.opts.Config.Log.RetentionDays)
		activity.Trim(h.opts.DB)
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeSettings(w http.ResponseWriter, r *http.Request) {
	if !h.nodeMode {
		http.Error(w, "node mode only", http.StatusBadRequest)
		return
	}
	_ = r.ParseForm()
	sid := h.localServerID()
	var ns storage.NodeSettings
	err := h.opts.DB.Where("server_id = ?", sid).First(&ns).Error
	if err != nil {
		ns = storage.NodeSettings{ServerID: sid}
	}
	ns.MainDomain = strings.ToLower(strings.TrimSpace(r.FormValue("main_domain")))
	ns.NS1 = strings.ToLower(strings.TrimSpace(r.FormValue("ns1")))
	ns.NS2 = strings.ToLower(strings.TrimSpace(r.FormValue("ns2")))
	ns.PublicIP = strings.TrimSpace(r.FormValue("public_ip"))
	if tok := strings.TrimSpace(r.FormValue("cf_api_token")); tok != "" {
		ns.CFAPIToken = tok
	}
	if ns.PublicIP == "" {
		ns.PublicIP = detectPublicIP(h.localExec())
	}
	flash := "Node identity saved"
	if ns.ID == 0 {
		if err := h.opts.DB.Create(&ns).Error; err != nil {
			flash = "Save failed: " + err.Error()
		}
	} else if err := h.opts.DB.Save(&ns).Error; err != nil {
		flash = "Save failed: " + err.Error()
	}
	if flash == "Node identity saved" {
		if err := h.EnsureServerWebmail(); err != nil {
			flash += " · server webmail: " + err.Error()
		}
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape(flash), http.StatusSeeOther)
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
