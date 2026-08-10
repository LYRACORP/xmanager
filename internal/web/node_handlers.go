package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/cron"
	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/notify"
	"github.com/lyracorp/xmanager/internal/project"
	"github.com/lyracorp/xmanager/internal/proxy"
	svcs "github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/services/bugsink"
	"github.com/lyracorp/xmanager/internal/services/gitea"
	"github.com/lyracorp/xmanager/internal/services/kafka"
	"github.com/lyracorp/xmanager/internal/services/mailinbox"
	"github.com/lyracorp/xmanager/internal/services/mattermost"
	"github.com/lyracorp/xmanager/internal/services/netdata"
	"github.com/lyracorp/xmanager/internal/services/powerdns"
	"github.com/lyracorp/xmanager/internal/services/rabbitmq"
	"github.com/lyracorp/xmanager/internal/services/registry"
	"github.com/lyracorp/xmanager/internal/services/rustfs"
	"github.com/lyracorp/xmanager/internal/services/umami"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

func (h *handler) registerNode(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.requireAuth(h.getNodeHome))
	mux.HandleFunc("GET /api/node/metrics", h.requireAuth(h.getNodeMetricsFragment))
	mux.HandleFunc("POST /api/node/ports/open", h.requireAuth(h.postNodePortsOpen))
	mux.HandleFunc("POST /api/node/ports/close", h.requireAuth(h.postNodePortsClose))

	mux.HandleFunc("GET /docker", h.requireAuth(h.getNodeDocker))
	mux.HandleFunc("POST /docker/{id}/start", h.requireAuth(h.postNodeDockerStart))
	mux.HandleFunc("POST /docker/{id}/stop", h.requireAuth(h.postNodeDockerStop))
	mux.HandleFunc("POST /docker/{id}/restart", h.requireAuth(h.postNodeDockerRestart))
	mux.HandleFunc("GET /docker/{id}/logs", h.requireAuth(h.getNodeDockerLogs))

	mux.HandleFunc("GET /cron", h.requireAuth(h.getNodeCron))
	mux.HandleFunc("POST /cron", h.requireAuth(h.postNodeCron))
	mux.HandleFunc("POST /cron/{id}/toggle", h.requireAuth(h.postNodeCronToggle))
	mux.HandleFunc("POST /cron/{id}/delete", h.requireAuth(h.postNodeCronDelete))
	mux.HandleFunc("GET /cron/{id}/logs", h.requireAuth(h.getNodeCronLogs))

	mux.HandleFunc("GET /projects", h.requireAuth(h.getNodeProjects))
	mux.HandleFunc("GET /projects/new", h.requireAuth(h.getNodeProjectsNew))
	mux.HandleFunc("POST /projects", h.requireAuth(h.postNodeProjects))
	// Catalog is outside /projects/{id}/… — ServeMux rejects overlapping wildcards
	// (e.g. /projects/oneclick/{id} vs /projects/{id}/deploy).
	mux.HandleFunc("GET /oneclick", h.requireAuth(h.getNodeProjectTemplates))
	mux.HandleFunc("GET /oneclick/{id}", h.requireAuth(h.getNodeProjectTemplate))
	mux.HandleFunc("POST /oneclick/{id}", h.requireAuth(h.postNodeProjectTemplate))
	mux.HandleFunc("GET /projects/{id}", h.requireAuth(h.getNodeProjectDetail))
	mux.HandleFunc("POST /projects/{id}/deploy", h.requireAuth(h.postNodeProjectDeploy))
	mux.HandleFunc("POST /projects/{id}/stop", h.requireAuth(h.postNodeProjectStop))
	mux.HandleFunc("POST /projects/{id}/restart", h.requireAuth(h.postNodeProjectRestart))
	mux.HandleFunc("POST /projects/{id}/domain", h.requireAuth(h.postNodeProjectDomain))
	mux.HandleFunc("POST /projects/{id}/env", h.requireAuth(h.postNodeProjectEnv))
	mux.HandleFunc("POST /projects/{id}/env/{env_id}/delete", h.requireAuth(h.postNodeProjectEnvDelete))
	mux.HandleFunc("POST /projects/{id}/delete", h.requireAuth(h.postNodeProjectDelete))

	mux.HandleFunc("GET /databases", h.requireAuth(h.getNodeDatabases))
	mux.HandleFunc("POST /databases", h.requireAuth(h.postNodeDatabases))
	mux.HandleFunc("POST /databases/user", h.requireAuth(h.postNodeDatabaseUser))
	mux.HandleFunc("POST /databases/backup", h.requireAuth(h.postNodeDatabaseBackup))
	mux.HandleFunc("POST /databases/link", h.requireAuth(h.postNodeDatabaseLink))

	mux.HandleFunc("GET /services", h.requireAuth(h.getNodeServices))
	mux.HandleFunc("POST /services/{name}/enable", h.requireAuth(h.postNodeServiceEnable))
	mux.HandleFunc("POST /services/{name}/disable", h.requireAuth(h.postNodeServiceDisable))
	mux.HandleFunc("POST /services/rustfs/bucket", h.requireAuth(h.postNodeRustfsBucket))

	mux.HandleFunc("GET /domains", h.requireAuth(h.getNodeDomains))
	mux.HandleFunc("POST /domains", h.requireAuth(h.postNodeDomains))
	mux.HandleFunc("POST /domains/connect", h.requireAuth(h.postNodeDomainConnect))
	mux.HandleFunc("POST /domains/mailbox", h.requireAuth(h.postNodeMailbox))
	mux.HandleFunc("POST /domains/mailbox/{id}/delete", h.requireAuth(h.postNodeMailboxDelete))

	mux.HandleFunc("GET /alerts", h.requireAuth(h.getNodeAlerts))
	mux.HandleFunc("POST /alerts/channel", h.requireAuth(h.postNodeAlertChannel))
	mux.HandleFunc("POST /alerts/channel/{id}/delete", h.requireAuth(h.postNodeAlertChannelDelete))
	mux.HandleFunc("POST /alerts/channel/{id}/test", h.requireAuth(h.postNodeAlertChannelTest))
	mux.HandleFunc("POST /alerts/monitor", h.requireAuth(h.postNodeAlertMonitor))
	mux.HandleFunc("POST /alerts/monitor/{id}/delete", h.requireAuth(h.postNodeAlertMonitorDelete))

	mux.HandleFunc("GET /netdata/", h.requireAuth(h.proxyNetdata))
	mux.HandleFunc("GET /netdata", h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/netdata/", http.StatusSeeOther)
	}))

	mux.HandleFunc("GET /settings", h.requireAuth(h.getSettings))
	mux.HandleFunc("POST /settings/git", h.requireAuth(h.postSettingsGit))
	mux.HandleFunc("GET /oauth/git/{provider}/connect", h.requireAuth(h.getOAuthGitConnect))
	mux.HandleFunc("GET /oauth/git/{provider}/callback", h.requireAuth(h.getOAuthGitCallback))
	mux.HandleFunc("POST /settings/git/{provider}/disconnect", h.requireAuth(h.postOAuthGitDisconnect))
	mux.HandleFunc("GET /api/git/repos", h.requireAuth(h.getAPIGitRepos))
	mux.HandleFunc("POST /webhook/{project_id}", h.postNodeWebhook)
}

func (h *handler) localExec() *ssh.Executor {
	if h.exec == nil {
		h.exec = ssh.NewLocalExecutor()
	}
	return h.exec
}

func (h *handler) localServerID() uint {
	if h.localSrvID == 0 && h.opts.DB != nil {
		if srv, err := EnsureLocalServer(h.opts.DB); err == nil {
			h.localSrvID = srv.ID
		}
	}
	return h.localSrvID
}

func (h *handler) basePage(sess *session, title string) pageData {
	return pageData{
		Title:     title,
		Session:   sess,
		NodeMode:  true,
		ServerID:  h.localServerID(),
		ActiveNav: "",
	}
}

// --- Docker ---

func (h *handler) getNodeDocker(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	mgr := docker.NewManager(h.localExec())
	list, err := mgr.ListContainers()
	data := h.basePage(sess, "Docker")
	data.ActiveNav = "docker"
	data.Containers = list
	if err != nil {
		data.Flash = err.Error()
	}
	h.render(w, "node_docker", data)
}

func (h *handler) postNodeDockerStart(w http.ResponseWriter, r *http.Request) {
	_ = docker.NewManager(h.localExec()).StartContainer(r.PathValue("id"))
	http.Redirect(w, r, "/docker", http.StatusSeeOther)
}

func (h *handler) postNodeDockerStop(w http.ResponseWriter, r *http.Request) {
	_ = docker.NewManager(h.localExec()).StopContainer(r.PathValue("id"))
	http.Redirect(w, r, "/docker", http.StatusSeeOther)
}

func (h *handler) postNodeDockerRestart(w http.ResponseWriter, r *http.Request) {
	_ = docker.NewManager(h.localExec()).RestartContainer(r.PathValue("id"))
	http.Redirect(w, r, "/docker", http.StatusSeeOther)
}

func (h *handler) getNodeDockerLogs(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	id := r.PathValue("id")
	logs, err := docker.NewManager(h.localExec()).ContainerLogs(id, 200)
	data := h.basePage(sess, "Container logs")
	data.ActiveNav = "docker"
	data.LogText = logs
	data.ContainerID = id
	if err != nil {
		data.Flash = err.Error()
	}
	h.render(w, "node_docker_logs", data)
}

// --- Cron ---

func (h *handler) getNodeCron(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	mgr := cron.NewManager(h.localExec(), h.opts.DB)
	jobs, _ := mgr.List(h.localServerID())
	data := h.basePage(sess, "Cron")
	data.ActiveNav = "cron"
	data.CronJobs = jobs
	h.render(w, "node_cron", data)
}

func (h *handler) postNodeCron(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	mgr := cron.NewManager(h.localExec(), h.opts.DB)
	_, err := mgr.Add(h.localServerID(), r.FormValue("name"), r.FormValue("expression"), r.FormValue("command"))
	if err != nil {
		sess := sessionFromCtx(r.Context())
		data := h.basePage(sess, "Cron")
		data.ActiveNav = "cron"
		data.Flash = err.Error()
		jobs, _ := mgr.List(h.localServerID())
		data.CronJobs = jobs
		h.render(w, "node_cron", data)
		return
	}
	http.Redirect(w, r, "/cron", http.StatusSeeOther)
}

func (h *handler) postNodeCronToggle(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var job storage.CronJob
	if h.opts.DB.First(&job, id).Error == nil {
		_ = cron.NewManager(h.localExec(), h.opts.DB).SetEnabled(uint(id), !job.Enabled)
	}
	http.Redirect(w, r, "/cron", http.StatusSeeOther)
}

func (h *handler) postNodeCronDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	_ = cron.NewManager(h.localExec(), h.opts.DB).Remove(uint(id))
	http.Redirect(w, r, "/cron", http.StatusSeeOther)
}

func (h *handler) getNodeCronLogs(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	runs, err := cron.NewManager(h.localExec(), h.opts.DB).Logs(h.localServerID(), uint(id))
	data := h.basePage(sess, "Cron logs")
	data.ActiveNav = "cron"
	data.CronRuns = runs
	data.CronJobID = uint(id)
	if err != nil {
		data.Flash = err.Error()
	}
	h.render(w, "node_cron_logs", data)
}

func (h *handler) postNodeWebhook(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseUint(r.PathValue("project_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return
	}
	var p storage.Project
	if err := h.opts.DB.First(&p, projectID).Error; err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if p.WebhookSecret != "" {
		secret := r.Header.Get("X-Webhook-Secret")
		if secret != p.WebhookSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	go func() {
		dep := project.NewDeployer(ssh.NewLocalExecutor(), h.opts.DB)
		_, _ = dep.Deploy(&p)
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

// --- Databases ---

func (h *handler) getNodeDatabases(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	exec := h.localExec()
	avail := dbmanager.DetectAvailable(exec)
	var dbs []nodeDBView
	for _, t := range []dbmanager.DBType{
		dbmanager.PostgreSQL, dbmanager.MySQL, dbmanager.MariaDB,
		dbmanager.MongoDB, dbmanager.ClickHouse, dbmanager.Redis,
	} {
		if !avail[t] {
			continue
		}
		mgr := dbmanager.NewManager(t, exec)
		list, _ := mgr.ListDatabases()
		users, _ := mgr.ListUsers()
		dbs = append(dbs, nodeDBView{Type: string(t), Databases: list, Users: users})
	}
	var links []storage.ProjectDatabase
	h.opts.DB.Where("server_id = ?", h.localServerID()).Find(&links)
	var projects []storage.Project
	h.opts.DB.Where("server_id = ?", h.localServerID()).Find(&projects)
	data := h.basePage(sess, "Databases")
	data.ActiveNav = "databases"
	data.NodeDBs = dbs
	data.ProjectDBs = links
	data.Projects = projects
	h.render(w, "node_databases", data)
}

func (h *handler) postNodeDatabases(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	t := dbmanager.DBType(r.FormValue("db_type"))
	mgr := dbmanager.NewManager(t, h.localExec())
	if mgr != nil {
		_ = mgr.CreateDatabase(r.FormValue("name"))
	}
	http.Redirect(w, r, "/databases", http.StatusSeeOther)
}

func (h *handler) postNodeDatabaseUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	t := dbmanager.DBType(r.FormValue("db_type"))
	mgr := dbmanager.NewManager(t, h.localExec())
	if mgr != nil {
		_ = mgr.CreateUser(r.FormValue("username"), r.FormValue("password"))
		_ = h.opts.DB.Create(&storage.DatabaseUser{
			ServerID:  h.localServerID(),
			DBType:    string(t),
			Username:  r.FormValue("username"),
			Databases: r.FormValue("databases"),
			Host:      "%",
		}).Error
	}
	http.Redirect(w, r, "/databases", http.StatusSeeOther)
}

func (h *handler) postNodeDatabaseBackup(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	t := dbmanager.DBType(r.FormValue("db_type"))
	name := r.FormValue("name")
	dest := fmt.Sprintf("/opt/xmanager/backups/%s-%s-%d.sql", t, name, time.Now().Unix())
	_, _ = h.localExec().Run("mkdir -p /opt/xmanager/backups")
	mgr := dbmanager.NewManager(t, h.localExec())
	if mgr != nil {
		_ = mgr.Backup(name, dest)
	}
	http.Redirect(w, r, "/databases", http.StatusSeeOther)
}

func (h *handler) postNodeDatabaseLink(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	pid, _ := strconv.ParseUint(r.FormValue("project_id"), 10, 64)
	_ = h.opts.DB.Create(&storage.ProjectDatabase{
		ProjectID: uint(pid),
		ServerID:  h.localServerID(),
		DBType:    r.FormValue("db_type"),
		DBName:    r.FormValue("db_name"),
		Username:  r.FormValue("username"),
	}).Error
	http.Redirect(w, r, "/databases", http.StatusSeeOther)
}

// --- Services ---

type nodeServiceView struct {
	Name    string
	Enabled bool
	Running bool
	Status  string
	Default bool // enabled by default on node install
}

func (h *handler) lookupNodeService(name string) svcs.Service {
	sid := h.localServerID()
	db := h.opts.DB
	switch name {
	case "registry":
		return registry.New(db, sid)
	case "gitea":
		return gitea.New(db, sid)
	case "rustfs":
		return rustfs.New(db, sid)
	case "rabbitmq":
		return rabbitmq.New(db, sid)
	case "kafka":
		return kafka.New(db, sid)
	case "mattermost":
		return mattermost.New(db, sid)
	case "bugsink":
		return bugsink.New(db, sid)
	case "umami":
		return umami.New(db, sid)
	case "powerdns":
		return powerdns.New(db, sid)
	case "mailinbox":
		return mailinbox.New(db, sid)
	case "netdata":
		return netdata.New(db, sid)
	default:
		return nil
	}
}

func (h *handler) getNodeServices(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	names := []string{
		"registry", "gitea", "rustfs", "rabbitmq", "kafka",
		"mattermost", "bugsink", "umami", "powerdns", "mailinbox", "netdata",
	}
	exec := h.localExec()
	var list []nodeServiceView
	for _, n := range names {
		svc := h.lookupNodeService(n)
		if svc == nil {
			continue
		}
		enabled := svc.IsEnabled(exec)
		status := svc.Status(exec)
		running := enabled && status != "stopped" && status != ""
		list = append(list, nodeServiceView{
			Name:    n,
			Enabled: enabled,
			Running: running,
			Status:  status,
			Default: isDefaultNodeService(n),
		})
	}
	data := h.basePage(sess, "Services")
	data.ActiveNav = "services"
	data.NodeServices = list
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.render(w, "node_services", data)
}

func (h *handler) postNodeServiceEnable(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := r.PathValue("name")
	svc := h.lookupNodeService(name)
	cfg := map[string]string{}
	if name == "netdata" {
		cfg["nginx_user"] = r.FormValue("nginx_user")
		cfg["nginx_pass"] = r.FormValue("nginx_pass")
		if cfg["nginx_user"] == "" {
			cfg["nginx_user"] = "admin"
		}
		if cfg["nginx_pass"] == "" {
			cfg["nginx_pass"] = "xmanager"
		}
	}
	if name == "mailinbox" {
		for _, k := range []string{"mode", "api_base", "admin_user", "admin_password", "hostname", "https_port"} {
			if v := strings.TrimSpace(r.FormValue(k)); v != "" {
				cfg[k] = v
			}
		}
	}
	if name == "powerdns" {
		for _, k := range []string{"api_key", "api_port", "dns_port"} {
			if v := strings.TrimSpace(r.FormValue(k)); v != "" {
				cfg[k] = v
			}
		}
	}
	if svc == nil {
		http.Redirect(w, r, "/services?flash=unknown+service", http.StatusSeeOther)
		return
	}
	if err := svc.Enable(h.localExec(), cfg); err != nil {
		http.Redirect(w, r, "/services?flash="+url.QueryEscape("enable failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/services?flash="+url.QueryEscape(name+" enabled"), http.StatusSeeOther)
}

func (h *handler) postNodeServiceDisable(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	svc := h.lookupNodeService(name)
	if svc == nil {
		http.Redirect(w, r, "/services", http.StatusSeeOther)
		return
	}
	if err := svc.Disable(h.localExec()); err != nil {
		http.Redirect(w, r, "/services?flash="+url.QueryEscape("disable failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/services?flash="+url.QueryEscape(name+" disabled"), http.StatusSeeOther)
}

func (h *handler) postNodeRustfsBucket(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("bucket"))
	if name != "" {
		// Best-effort: rustfs/minio-compatible mc inside the container or host.
		_, _ = h.localExec().Run(fmt.Sprintf(
			`docker exec rustfs mc mb local/%s 2>/dev/null || docker exec rustfs mkdir -p /data/%s 2>/dev/null || mkdir -p /opt/xmanager/services/rustfs/data/%s`,
			name, name, name,
		))
	}
	http.Redirect(w, r, "/services", http.StatusSeeOther)
}

// --- Domains & mailboxes (PowerDNS + mail APIs) ---

func (h *handler) getNodeDomains(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	sid := h.localServerID()
	var domains []storage.ProjectDomain
	h.opts.DB.Order("domain asc").Find(&domains)
	var connected []storage.ConnectedDomain
	h.opts.DB.Where("server_id = ?", sid).Order("domain asc").Find(&connected)
	var mailboxes []storage.Mailbox
	h.opts.DB.Where("server_id = ?", sid).Order("address asc").Find(&mailboxes)
	var projects []storage.Project
	h.opts.DB.Where("server_id = ?", sid).Find(&projects)
	var vhosts []proxy.VHost
	if m := proxy.NewManager(proxy.Nginx, h.localExec()); m != nil {
		vhosts, _ = m.ListVHosts()
	}
	mailCfg := mailinbox.LoadConfig(h.opts.DB, sid)
	pdnsOK := powerdns.NewClient(powerdns.LoadConfig(h.opts.DB, sid)).Ping() == nil
	mailOK := mailinbox.NewClient(mailCfg).Ping() == nil

	data := h.basePage(sess, "Domains")
	data.ActiveNav = "domains"
	data.ProjectDomains = domains
	data.ConnectedDomains = connected
	data.Mailboxes = mailboxes
	data.Projects = projects
	data.VHosts = vhosts
	data.MailAPIMode = mailCfg.Mode
	data.PowerDNSReady = pdnsOK
	data.MailAPIReady = mailOK
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.render(w, "node_domains", data)
}

func (h *handler) postNodeDomains(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := strings.TrimSpace(r.FormValue("domain"))
	upstream := strings.TrimSpace(r.FormValue("upstream"))
	pid, _ := strconv.ParseUint(r.FormValue("project_id"), 10, 64)
	dbType := strings.TrimSpace(r.FormValue("db_type"))
	dbName := strings.TrimSpace(r.FormValue("db_name"))
	publicIP := strings.TrimSpace(r.FormValue("public_ip"))

	cd, err := h.connectDomain(domainConnectOpts{
		Domain:    domain,
		Upstream:  upstream,
		ProjectID: uint(pid),
		DBType:    dbType,
		DBName:    dbName,
		PublicIP:  publicIP,
	})
	flash := "Domain connected"
	if cd != nil && cd.DNSReady {
		flash += " · DNS zone ready"
	}
	if cd != nil && cd.MailReady {
		flash += " · mail domain ready"
	}
	if err != nil {
		flash = "Domain saved with warnings: " + err.Error()
	}
	http.Redirect(w, r, "/domains?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeDomainConnect(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := strings.TrimSpace(r.FormValue("domain"))
	pid, _ := strconv.ParseUint(r.FormValue("project_id"), 10, 64)
	dbType := strings.TrimSpace(r.FormValue("db_type"))
	dbName := strings.TrimSpace(r.FormValue("db_name"))
	_, err := h.connectDomain(domainConnectOpts{
		Domain:    domain,
		ProjectID: uint(pid),
		DBType:    dbType,
		DBName:    dbName,
		Upstream:  strings.TrimSpace(r.FormValue("upstream")),
		SkipNginx: r.FormValue("skip_nginx") == "1",
	})
	flash := "Links updated for " + domain
	if err != nil {
		flash = err.Error()
	}
	http.Redirect(w, r, "/domains?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeMailbox(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	local := strings.TrimSpace(r.FormValue("local_part"))
	domain := strings.TrimSpace(r.FormValue("domain"))
	password := r.FormValue("password")
	pid, _ := strconv.ParseUint(r.FormValue("project_id"), 10, 64)
	_, err := h.createMailboxAPI(local, domain, password, uint(pid))
	flash := "Mailbox created via mail API"
	if err != nil {
		flash = "Mailbox failed: " + err.Error()
	}
	http.Redirect(w, r, "/domains?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeMailboxDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	flash := "Mailbox deleted"
	if err := h.deleteMailboxAPI(uint(id)); err != nil {
		flash = err.Error()
	}
	http.Redirect(w, r, "/domains?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}

// --- Alerts ---

func (h *handler) getNodeAlerts(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	var channels []storage.AlertChannel
	h.opts.DB.Order("name asc").Find(&channels)
	var monitors []storage.UptimeMonitor
	h.opts.DB.Where("server_id = ?", h.localServerID()).Order("name asc").Find(&monitors)
	data := h.basePage(sess, "Alerts")
	data.ActiveNav = "alerts"
	data.AlertChannels = channels
	data.Monitors = monitors
	h.render(w, "node_alerts", data)
}

func (h *handler) postNodeAlertChannel(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	cfg := map[string]string{
		"bot_token": r.FormValue("bot_token"),
		"chat_id":   r.FormValue("chat_id"),
		"url":       r.FormValue("url"),
		"smtp_host": r.FormValue("smtp_host"),
		"smtp_port": r.FormValue("smtp_port"),
		"username":  r.FormValue("username"),
		"password":  r.FormValue("password"),
		"from":      r.FormValue("from"),
		"to":        r.FormValue("to"),
		"api_key":   r.FormValue("api_key"),
		"from_num":  r.FormValue("from_num"),
		"to_num":    r.FormValue("to_num"),
	}
	b, _ := json.Marshal(cfg)
	_ = h.opts.DB.Create(&storage.AlertChannel{
		Name:       r.FormValue("name"),
		Type:       r.FormValue("type"),
		ConfigJSON: string(b),
		Enabled:    true,
	}).Error
	http.Redirect(w, r, "/alerts", http.StatusSeeOther)
}

func (h *handler) postNodeAlertChannelDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	h.opts.DB.Delete(&storage.AlertChannel{}, id)
	http.Redirect(w, r, "/alerts", http.StatusSeeOther)
}

func (h *handler) postNodeAlertChannelTest(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var ch storage.AlertChannel
	if h.opts.DB.First(&ch, id).Error == nil {
		if n := channelToNotifier(ch); n != nil {
			_ = n.Test()
		}
	}
	http.Redirect(w, r, "/alerts", http.StatusSeeOther)
}

func (h *handler) postNodeAlertMonitor(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	interval, _ := strconv.Atoi(r.FormValue("interval_sec"))
	if interval <= 0 {
		interval = 60
	}
	_ = h.opts.DB.Create(&storage.UptimeMonitor{
		Name:          r.FormValue("name"),
		ServerID:      h.localServerID(),
		URL:           r.FormValue("url"),
		IntervalSec:   interval,
		AlertChannels: r.FormValue("alert_channels"),
		Enabled:       true,
	}).Error
	http.Redirect(w, r, "/alerts", http.StatusSeeOther)
}

func (h *handler) postNodeAlertMonitorDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	h.opts.DB.Delete(&storage.UptimeMonitor{}, id)
	http.Redirect(w, r, "/alerts", http.StatusSeeOther)
}

func channelToNotifier(ch storage.AlertChannel) notify.Notifier {
	var cfg map[string]string
	_ = json.Unmarshal([]byte(ch.ConfigJSON), &cfg)
	switch ch.Type {
	case "telegram":
		return notify.NewTelegram(cfg["bot_token"], cfg["chat_id"])
	case "webhook":
		return notify.NewWebhook(cfg["url"], nil)
	case "email":
		to := strings.Split(cfg["to"], ",")
		port, _ := strconv.Atoi(cfg["smtp_port"])
		return notify.NewEmail(cfg["smtp_host"], port, cfg["username"], cfg["password"], cfg["from"], to)
	case "sms":
		return notify.NewSMS(cfg["url"], cfg["api_key"], cfg["to_num"])
	default:
		return nil
	}
}

// proxyNetdata reverse-proxies local netdata behind panel auth.
func (h *handler) proxyNetdata(w http.ResponseWriter, r *http.Request) {
	target, _ := url.Parse("http://127.0.0.1:19999")
	rp := httputil.NewSingleHostReverseProxy(target)
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/netdata")
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}
	rp.ServeHTTP(w, r)
}
