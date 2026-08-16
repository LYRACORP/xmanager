package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/backup"
	"github.com/lyracorp/xmanager/internal/cron"
	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/hostfirewall"
	"github.com/lyracorp/xmanager/internal/notify"
	"github.com/lyracorp/xmanager/internal/project"
	"github.com/lyracorp/xmanager/internal/proxy"
	svcs "github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/services/bugsink"
	"github.com/lyracorp/xmanager/internal/services/databasus"
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
	"github.com/lyracorp/xmanager/internal/services/uptimekuma"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

func (h *handler) registerNode(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.requireAuth(h.getNodeHome))
	mux.HandleFunc("GET /api/node/metrics", h.requireAuth(h.getNodeMetricsFragment))
	mux.HandleFunc("GET /api/node/disk-usage", h.requireAuth(h.getNodeDiskUsageFragment))
	mux.HandleFunc("GET /api/node/net.json", h.requireAuth(h.getNodeNetJSON))
	mux.HandleFunc("POST /api/node/ports/open", h.requireAuth(h.postNodePortsOpen))
	mux.HandleFunc("POST /api/node/ports/close", h.requireAuth(h.postNodePortsClose))

	mux.HandleFunc("GET /docker", h.requireAuth(h.getNodeDocker))
	mux.HandleFunc("POST /docker/{id}/start", h.requireAuth(h.postNodeDockerStart))
	mux.HandleFunc("POST /docker/{id}/stop", h.requireAuth(h.postNodeDockerStop))
	mux.HandleFunc("POST /docker/{id}/restart", h.requireAuth(h.postNodeDockerRestart))
	mux.HandleFunc("GET /docker/{id}/logs", h.requireAuth(h.getNodeDockerLogs))
	mux.HandleFunc("GET /docker/{id}/terminal", h.requireAuth(h.getNodeDockerTerminal))
	mux.HandleFunc("GET /docker/{id}/terminal/ws", h.requireAuthWS(h.getNodeDockerTerminalWS))

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
	mux.HandleFunc("GET /projects/{id}/files/download", h.requireAuth(h.getNodeProjectFilesDownload))
	mux.HandleFunc("POST /projects/{id}/files/upload", h.requireAuth(h.postNodeProjectFilesUpload))
	mux.HandleFunc("POST /projects/{id}/files/mkdir", h.requireAuth(h.postNodeProjectFilesMkdir))
	mux.HandleFunc("POST /projects/{id}/files/create", h.requireAuth(h.postNodeProjectFilesCreate))
	mux.HandleFunc("POST /projects/{id}/files/save", h.requireAuth(h.postNodeProjectFilesSave))
	mux.HandleFunc("POST /projects/{id}/files/rename", h.requireAuth(h.postNodeProjectFilesRename))
	mux.HandleFunc("POST /projects/{id}/files/delete", h.requireAuth(h.postNodeProjectFilesDelete))
	mux.HandleFunc("POST /projects/{id}/files/chmod", h.requireAuth(h.postNodeProjectFilesChmod))
	mux.HandleFunc("GET /projects/{id}/terminal/ws", h.requireAuthWS(h.getNodeProjectTerminalWS))
	mux.HandleFunc("POST /projects/{id}/deploy", h.requireAuth(h.postNodeProjectDeploy))
	mux.HandleFunc("POST /projects/{id}/stop", h.requireAuth(h.postNodeProjectStop))
	mux.HandleFunc("POST /projects/{id}/restart", h.requireAuth(h.postNodeProjectRestart))
	mux.HandleFunc("POST /projects/{id}/domain", h.requireAuth(h.postNodeProjectDomain))
	mux.HandleFunc("POST /projects/{id}/env", h.requireAuth(h.postNodeProjectEnv))
	mux.HandleFunc("POST /projects/{id}/env/{env_id}/delete", h.requireAuth(h.postNodeProjectEnvDelete))
	mux.HandleFunc("POST /projects/{id}/delete", h.requireAuth(h.postNodeProjectDelete))

	mux.HandleFunc("GET /databases", h.requireAuth(h.getNodeDatabases))
	mux.HandleFunc("GET /databases/{type}/{name}", h.requireAuth(h.getNodeDatabaseDetail))
	mux.HandleFunc("GET /api/databases/{type}/stats", h.requireAuth(h.getAPIDatabaseEngineStats))
	mux.HandleFunc("GET /api/databases/{type}/{name}/metrics", h.requireAuth(h.getAPIDatabaseDetailMetrics))
	mux.HandleFunc("POST /databases", h.requireAuth(h.postNodeDatabases))
	mux.HandleFunc("POST /databases/install", h.requireAuth(h.postNodeDatabaseInstall))
	mux.HandleFunc("POST /databases/user", h.requireAuth(h.postNodeDatabaseUser))
	mux.HandleFunc("POST /databases/backup", h.requireAuth(h.postNodeDatabaseBackup))
	mux.HandleFunc("POST /databases/link", h.requireAuth(h.postNodeDatabaseLink))
	mux.HandleFunc("POST /databases/tools/adminer", h.requireAuth(h.postNodeDatabaseToolAdminer))
	mux.HandleFunc("POST /databases/tools/pgadmin", h.requireAuth(h.postNodeDatabaseToolPgAdmin))

	mux.HandleFunc("GET /backup", h.requireAuth(h.getNodeBackup))
	mux.HandleFunc("POST /backup/run", h.requireAuth(h.postNodeBackupRun))
	mux.HandleFunc("POST /backup/schedule", h.requireAuth(h.postNodeBackupSchedule))
	mux.HandleFunc("POST /backup/schedule/{id}/delete", h.requireAuth(h.postNodeBackupScheduleDelete))
	mux.HandleFunc("POST /backup/schedule/{id}/update", h.requireAuth(h.postNodeBackupScheduleUpdate))
	mux.HandleFunc("POST /backup/schedule/{id}/run", h.requireAuth(h.postNodeBackupScheduleRun))
	mux.HandleFunc("POST /backup/destinations", h.requireAuth(h.postNodeBackupDestination))
	mux.HandleFunc("POST /backup/destinations/{id}/delete", h.requireAuth(h.postNodeBackupDestinationDelete))
	mux.HandleFunc("GET /backup/{id}/download", h.requireAuth(h.getNodeBackupDownload))
	mux.HandleFunc("POST /backup/{id}/delete", h.requireAuth(h.postNodeBackupDelete))
	mux.HandleFunc("POST /backup/{id}/resend", h.requireAuth(h.postNodeBackupResend))

	mux.HandleFunc("GET /logs", h.requireAuth(h.getNodeLogs))
	mux.HandleFunc("GET /api/node/logs", h.requireAuth(h.getNodeLogsFragment))

	mux.HandleFunc("GET /services", h.requireAuth(h.getNodeServices))
	mux.HandleFunc("POST /services/{name}/enable", h.requireAuth(h.postNodeServiceEnable))
	mux.HandleFunc("POST /services/{name}/disable", h.requireAuth(h.postNodeServiceDisable))
	mux.HandleFunc("POST /services/rustfs/bucket", h.requireAuth(h.postNodeRustfsBucket))

	mux.HandleFunc("GET /storage", h.requireAuth(h.getNodeStorage))
	mux.HandleFunc("POST /storage/buckets", h.requireAuth(h.postNodeStorageBucketCreate))
	mux.HandleFunc("POST /storage/buckets/delete", h.requireAuth(h.postNodeStorageBucketDelete))
	mux.HandleFunc("GET /storage/buckets/{bucket}", h.requireAuth(h.getNodeStorageBucket))
	mux.HandleFunc("GET /storage/buckets/{bucket}/download", h.requireAuth(h.getNodeStorageDownload))
	mux.HandleFunc("POST /storage/buckets/{bucket}/upload", h.requireAuth(h.postNodeStorageUpload))
	mux.HandleFunc("POST /storage/buckets/{bucket}/mkdir", h.requireAuth(h.postNodeStorageMkdir))
	mux.HandleFunc("POST /storage/buckets/{bucket}/delete", h.requireAuth(h.postNodeStorageDelete))
	mux.HandleFunc("POST /storage/buckets/{bucket}/rename", h.requireAuth(h.postNodeStorageRename))

	mux.HandleFunc("GET /domains", h.requireAuth(h.getNodeDomains))
	mux.HandleFunc("POST /domains", h.requireAuth(h.postNodeDomains))
	mux.HandleFunc("POST /domains/connect", h.requireAuth(h.postNodeDomainConnect))
	mux.HandleFunc("POST /domains/mailbox", h.requireAuth(h.postNodeMailbox))
	mux.HandleFunc("POST /domains/mailbox/{id}/delete", h.requireAuth(h.postNodeMailboxDelete))

	mux.HandleFunc("GET /dns", h.requireAuth(h.getNodeDNS))
	mux.HandleFunc("POST /dns/zones", h.requireAuth(h.postNodeDNSZone))
	mux.HandleFunc("POST /dns/zones/{zone}/delete", h.requireAuth(h.postNodeDNSZoneDelete))
	mux.HandleFunc("GET /dns/zones/{zone}", h.requireAuth(h.getNodeDNSZoneDetail))
	mux.HandleFunc("POST /dns/zones/{zone}/records", h.requireAuth(h.postNodeDNSRecord))
	mux.HandleFunc("POST /dns/zones/{zone}/records/delete", h.requireAuth(h.postNodeDNSRecordDelete))
	mux.HandleFunc("POST /dns/zones/{zone}/link", h.requireAuth(h.postNodeDNSZoneLink))

	mux.HandleFunc("GET /email", h.requireAuth(h.getNodeEmail))
	mux.HandleFunc("POST /email/domains", h.requireAuth(h.postNodeEmailDomain))
	mux.HandleFunc("POST /email/accounts", h.requireAuth(h.postNodeEmailAccount))
	mux.HandleFunc("POST /email/accounts/delete", h.requireAuth(h.postNodeEmailAccountDelete))
	mux.HandleFunc("POST /email/config", h.requireAuth(h.postNodeEmailConfig))

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
	mux.HandleFunc("POST /oauth/git/{provider}/token", h.requireAuth(h.postOAuthGitToken))
	mux.HandleFunc("GET /oauth/git/{provider}/callback", h.requireAuth(h.getOAuthGitCallback))
	mux.HandleFunc("GET /oauth/git/{provider}/relay-finish", h.requireAuth(h.getOAuthGitRelayFinish))
	mux.HandleFunc("GET /settings/git/{provider}/device/poll", h.requireAuth(h.getDevicePoll))
	mux.HandleFunc("POST /settings/git/{provider}/disconnect", h.requireAuth(h.postOAuthGitDisconnect))
	mux.HandleFunc("GET /api/git/repos", h.requireAuth(h.getAPIGitRepos))
	mux.HandleFunc("GET /api/git/device/start", h.requireAuth(h.getAPIGitDeviceStart))
	mux.HandleFunc("GET /api/projects/{id}/stats", h.requireAuth(h.getAPIProjectStats))
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

type cronJobView struct {
	storage.CronJob
	TaskTypeLabel string
	TaskSummary   string
}

func cronJobsToViews(jobs []storage.CronJob) []cronJobView {
	out := make([]cronJobView, 0, len(jobs))
	for _, j := range jobs {
		v := cronJobView{CronJob: j}
		if j.TaskType != "" {
			v.TaskTypeLabel = backup.TaskTypeLabel(j.TaskType)
		} else {
			v.TaskTypeLabel = "Shell Script"
		}
		cfg := backup.ParseTaskConfig(j.TaskConfig)
		switch j.TaskType {
		case backup.TaskBackupDirectory, backup.TaskCutLog:
			v.TaskSummary = cfg.Path
		case backup.TaskAccessURL:
			v.TaskSummary = cfg.URL
		case backup.TaskShell:
			v.TaskSummary = truncateStr(cfg.Script, 60)
			if v.TaskSummary == "" {
				v.TaskSummary = truncateStr(j.Command, 60)
			}
		case backup.TaskBackupDatabase, "postgres", "mysql", "mariadb", "mongodb", "all":
			if j.TaskType == "all" || strings.Contains(j.Command, "datistemplate") {
				v.TaskSummary = "all databases"
			} else {
				v.TaskSummary = truncateStr(j.Command, 60)
			}
		default:
			v.TaskSummary = truncateStr(j.Command, 60)
		}
		out = append(out, v)
	}
	return out
}

func truncateStr(s string, n int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (h *handler) getNodeCron(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	mgr := cron.NewManager(h.localExec(), h.opts.DB)
	jobs, _ := mgr.List(h.localServerID())
	data := h.basePage(sess, "Cron")
	data.ActiveNav = "cron"
	data.CronJobs = jobs
	data.CronJobViews = cronJobsToViews(jobs)
	for _, t := range backup.ListDumpable(h.localExec()) {
		data.BackupTargets = append(data.BackupTargets, backupTargetView{
			Type: string(t.Type),
			Name: t.Name,
			Key:  string(t.Type) + ":" + t.Name,
		})
	}
	h.render(w, "node_cron", data)
}

func (h *handler) postNodeCron(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	mgr := cron.NewManager(h.localExec(), h.opts.DB)

	expr := strings.TrimSpace(r.FormValue("expression"))
	var err error
	if expr == "" && r.FormValue("freq") != "" {
		hs, perr := cron.ParseHumanForm(
			r.FormValue("freq"),
			r.FormValue("interval"),
			r.FormValue("hour"),
			r.FormValue("minute"),
			r.FormValue("weekday"),
			r.FormValue("month_day"),
		)
		if perr != nil {
			err = perr
		} else {
			expr, err = cron.ExpressionFromHuman(hs)
		}
	}

	name := strings.TrimSpace(r.FormValue("name"))
	taskType := strings.TrimSpace(r.FormValue("task_type"))
	command := strings.TrimSpace(r.FormValue("command"))
	taskConfig := ""

	if err == nil && taskType != "" {
		cfg := backup.TaskConfig{Name: name}
		service := name
		switch taskType {
		case backup.TaskBackupDatabase:
			target := strings.TrimSpace(r.FormValue("target"))
			if target == "" || target == "all" {
				service = "__all__"
				taskType = "all"
			} else {
				parts := strings.SplitN(target, ":", 2)
				if len(parts) == 2 {
					taskType, service = parts[0], parts[1]
				} else {
					service = target
				}
			}
		case backup.TaskBackupDirectory:
			cfg.Path = strings.TrimSpace(r.FormValue("dir_path"))
			cfg.Compress = r.FormValue("compress") == "1"
			if service == "" {
				service = "directory"
			}
		case backup.TaskCutLog:
			cfg.Path = strings.TrimSpace(r.FormValue("log_path"))
			cfg.KeepLines = backup.ParseKeepLines(r.FormValue("keep_lines"))
			if service == "" {
				service = "cut-log"
			}
		case backup.TaskAccessURL:
			cfg.URL = strings.TrimSpace(r.FormValue("url"))
			cfg.Timeout, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("timeout")))
			if cfg.Timeout <= 0 {
				cfg.Timeout = 10
			}
			if service == "" {
				service = cfg.URL
			}
		case backup.TaskShell:
			cfg.Script = r.FormValue("script")
			if strings.TrimSpace(cfg.Script) == "" {
				cfg.Script = command
			}
			if service == "" {
				service = "shell"
			}
		case backup.TaskFullBackup, backup.TaskSyncTime, backup.TaskFreeRAM:
			if service == "" {
				service = taskType
			}
		default:
			err = fmt.Errorf("unknown task type")
		}
		if err == nil {
			command, err = backup.BuildHostCommand(taskType, service, cfg)
			taskConfig = cfg.JSON()
		}
	} else if err == nil && command == "" {
		// Legacy advanced form: raw command only.
		err = fmt.Errorf("command required")
	}

	if err == nil {
		if taskType == "" {
			taskType = backup.TaskShell
			taskConfig = backup.TaskConfig{Name: name, Script: command}.JSON()
		}
		_, err = mgr.AddTask(h.localServerID(), name, expr, command, taskType, taskConfig)
	}
	if err != nil {
		sess := sessionFromCtx(r.Context())
		data := h.basePage(sess, "Cron")
		data.ActiveNav = "cron"
		data.Flash = err.Error()
		jobs, _ := mgr.List(h.localServerID())
		data.CronJobs = jobs
		data.CronJobViews = cronJobsToViews(jobs)
		for _, t := range backup.ListDumpable(h.localExec()) {
			data.BackupTargets = append(data.BackupTargets, backupTargetView{
				Type: string(t.Type),
				Name: t.Name,
				Key:  string(t.Type) + ":" + t.Name,
			})
		}
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

func publicHost(r *http.Request) string {
	host := r.Host
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		return "localhost"
	}
	return host
}

func (h *handler) getNodeDatabases(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	exec := h.localExec()
	avail := dbmanager.DetectAvailable(exec)
	availMap := map[string]bool{}
	for _, t := range []dbmanager.DBType{
		dbmanager.PostgreSQL, dbmanager.MySQL, dbmanager.MariaDB,
		dbmanager.MongoDB, dbmanager.ClickHouse, dbmanager.Redis,
	} {
		availMap[string(t)] = avail[t]
	}

	pubHost := publicHost(r)
	// Lightweight: attach tools/engines to xm-db so Adminer/pgAdmin can resolve xm-postgres.
	// Never rebuild/reinstall on GET (that hung the page).
	dbmanager.EnsureDBToolNetworking(exec)
	adminerOn := dbmanager.ContainerRunning(exec, dbmanager.AdminerName)
	pgAdminOn := dbmanager.ContainerRunning(exec, dbmanager.PgAdminName)
	var toolHints []string
	if adminerOn && dbmanager.AdminerNeedsUpgrade(exec) {
		toolHints = append(toolHints, "Adminer image is outdated — click ↻ next to Adminer to rebuild")
	}
	if pgAdminOn {
		if dbmanager.PgAdminNeedsUpgrade(exec) || dbmanager.PgAdminNeedsRepair(exec) {
			toolHints = append(toolHints, "pgAdmin needs repair — click ↻ next to pgAdmin")
			pgAdminOn = false // don't show Open link until UI is actually usable
		} else if dbmanager.PgAdminConfigStale(exec) {
			toolHints = append(toolHints, "pgAdmin config stale — click ↻ next to pgAdmin to reload servers")
		}
	}
	adminerURL := fmt.Sprintf("http://%s:%s", pubHost, dbmanager.AdminerPort)
	pgAdminURL := fmt.Sprintf("http://%s:%s", pubHost, dbmanager.PgAdminPort)
	dbTools := []nodeDBToolView{
		{Name: "adminer", Container: dbmanager.AdminerName, Port: dbmanager.AdminerPort, URL: adminerURL, Running: adminerOn},
		{Name: "pgadmin", Container: dbmanager.PgAdminName, Port: dbmanager.PgAdminPort, URL: pgAdminURL, Running: pgAdminOn},
	}

	dockerMgr := docker.NewManager(exec)
	allContainers, _ := dockerMgr.ListContainers()
	byName := map[string]docker.Container{}
	statNames := []string{}
	for _, c := range allContainers {
		byName[c.Name] = c
		if c.State == "running" && strings.HasPrefix(c.Name, "xm-") {
			statNames = append(statNames, c.Name)
		}
	}
	stats := dockerMgr.Stats(statNames)

	var dbs []nodeDBView
	var listErrs []string
	for _, t := range []dbmanager.DBType{
		dbmanager.PostgreSQL, dbmanager.MySQL, dbmanager.MariaDB,
		dbmanager.MongoDB, dbmanager.ClickHouse, dbmanager.Redis,
	} {
		if !avail[t] {
			continue
		}
		mgr := dbmanager.NewManager(t, exec)
		list, err := mgr.ListDatabases()
		if err != nil {
			listErrs = append(listErrs, string(t)+": "+err.Error())
		}
		users, _ := mgr.ListUsers()

		containerName := dbmanager.ContainerName(t)
		view := nodeDBView{
			Type:       string(t),
			Container:  containerName,
			Host:       containerName,
			Port:       dbmanager.DefaultPort(t),
			Running:    true,
			AdminerURL: adminerURL,
			AdminerOn:  adminerOn,
			Databases:  list,
			Users:      users,
		}
		if t == dbmanager.PostgreSQL {
			view.PgAdminURL = pgAdminURL
			view.PgAdminOn = pgAdminOn
		}
		if c, ok := byName[containerName]; ok {
			view.Image = c.Image
			view.Ports = c.Ports
			view.ContainerID = c.ID
			view.Running = c.State == "running"
			if p := dbmanager.ParseHostPort(c.Ports); p != "" {
				view.Port = p
			}
			if c.ID != "" {
				view.LogsURL = "/docker/" + c.ID + "/logs"
			}
			if st, ok := stats[containerName]; ok {
				view.CPUPct = st.CPUPct
				view.MemUsage = st.MemUsage
			}
		}
		dbs = append(dbs, view)
	}
	var links []storage.ProjectDatabase
	h.opts.DB.Where("server_id = ?", h.localServerID()).Find(&links)
	var projects []storage.Project
	h.opts.DB.Where("server_id = ?", h.localServerID()).Find(&projects)
	data := h.basePage(sess, "Databases")
	data.ActiveNav = "databases"
	data.NodeDBs = dbs
	data.DBTools = dbTools
	data.DBAvailable = availMap
	data.DBEngines = []string{"postgres", "mysql", "mariadb", "mongodb", "redis", "clickhouse"}
	data.ProjectDBs = links
	data.Projects = projects
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	if len(toolHints) > 0 {
		hint := strings.Join(toolHints, " · ")
		if data.Flash != "" {
			data.Flash += " · " + hint
		} else {
			data.Flash = hint
		}
	}
	if len(listErrs) > 0 {
		msg := "list error: " + strings.Join(listErrs, "; ")
		if data.Flash != "" {
			data.Flash += " · " + msg
		} else {
			data.Flash = msg
		}
	}
	h.render(w, "node_databases", data)
}

func (h *handler) postNodeDatabases(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	t := dbmanager.DBType(r.FormValue("db_type"))
	name := strings.TrimSpace(r.FormValue("name"))
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))
	if name == "" {
		http.Redirect(w, r, "/databases?flash="+urlQueryEscape("database name required"), http.StatusSeeOther)
		return
	}
	mgr := dbmanager.NewManager(t, h.localExec())
	if mgr == nil {
		http.Redirect(w, r, "/databases?flash="+urlQueryEscape("unknown db type: "+string(t)), http.StatusSeeOther)
		return
	}
	user, err := dbmanager.ProvisionDatabase(mgr, name, username, password)
	if err != nil {
		http.Redirect(w, r, "/databases?flash="+urlQueryEscape("create failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	flash := `database "` + name + `" created`
	if password != "" && user != "" {
		flash += fmt.Sprintf(" · user %s / %s (host: %s)", user, password, dbmanager.ContainerName(t))
		_ = h.opts.DB.Create(&storage.DatabaseUser{
			ServerID:  h.localServerID(),
			DBType:    string(t),
			Username:  user,
			Databases: name,
			Host:      "%",
		}).Error
	} else if password == "" {
		flash += " · no login user (password was empty)"
	}
	http.Redirect(w, r, "/databases?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

// postNodeDatabaseInstall installs a database engine as a Docker container
// (with optional version, extensions, pgAdmin4, Adminer), then creates the first DB.
func (h *handler) postNodeDatabaseInstall(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	dbType := strings.TrimSpace(r.FormValue("db_type"))
	dbName := strings.TrimSpace(r.FormValue("name"))
	dbUser := strings.TrimSpace(r.FormValue("username"))
	dbPass := strings.TrimSpace(r.FormValue("password"))
	version := strings.TrimSpace(r.FormValue("version"))
	pgvector := r.FormValue("ext_pgvector") == "1"
	postgis := r.FormValue("ext_postgis") == "1"
	pgadmin := r.FormValue("pgadmin") == "1"
	adminer := r.FormValue("adminer") == "1"
	pgaEmail := strings.TrimSpace(r.FormValue("pgadmin_email"))
	pgaPass := strings.TrimSpace(r.FormValue("pgadmin_pass"))
	exec := h.localExec()

	dbmanager.EnsureDBNetwork(exec)
	net := dbmanager.NetworkFlag()
	rootPass := randomToken()[:20]
	var cmds []string
	var flash string

	switch dbType {
	case "postgres":
		if version == "" {
			version = "17"
		}
		image := "postgres:" + version + "-alpine"
		if pgvector && postgis {
			image = fmt.Sprintf("postgis/postgis:%s-3.5", version)
		} else if postgis {
			image = fmt.Sprintf("postgis/postgis:%s-3.5", version)
		} else if pgvector {
			image = fmt.Sprintf("pgvector/pgvector:pg%s", version)
		}
		cmds = append(cmds, fmt.Sprintf(
			`docker run -d --name xm-postgres --restart unless-stopped %s`+
				` -e POSTGRES_PASSWORD=%s -p 5432:5432`+
				` -v xm-postgres-data:/var/lib/postgresql/data %s 2>&1`,
			net, rootPass, image,
		))
		if pgvector && postgis {
			cmds = append(cmds,
				`sleep 8`,
				`docker exec xm-postgres bash -c "apt-get update -qq && apt-get install -y postgresql-`+version+`-pgvector 2>&1" || true`,
			)
		}
		flash = fmt.Sprintf("PostgreSQL %s started (password: %s · host: xm-postgres)", version, rootPass)
		if pgvector {
			flash += " + pgvector"
		}
		if postgis {
			flash += " + PostGIS"
		}

	case "mysql":
		if version == "" {
			version = "9.0"
		}
		cmds = append(cmds, fmt.Sprintf(
			`docker run -d --name xm-mysql --restart unless-stopped %s`+
				` -e MYSQL_ROOT_PASSWORD=%s -p 3306:3306`+
				` -v xm-mysql-data:/var/lib/mysql mysql:%s 2>&1`,
			net, rootPass, version,
		))
		flash = fmt.Sprintf("MySQL %s started (password: %s · host: xm-mysql)", version, rootPass)

	case "mariadb":
		if version == "" {
			version = "11.4"
		}
		cmds = append(cmds, fmt.Sprintf(
			`docker run -d --name xm-mariadb --restart unless-stopped %s`+
				` -e MARIADB_ROOT_PASSWORD=%s -p 3306:3306`+
				` -v xm-mariadb-data:/var/lib/mysql mariadb:%s 2>&1`,
			net, rootPass, version,
		))
		flash = fmt.Sprintf("MariaDB %s started (password: %s · host: xm-mariadb)", version, rootPass)

	case "mongodb":
		if version == "" {
			version = "8.0"
		}
		cmds = append(cmds, fmt.Sprintf(
			`docker run -d --name xm-mongodb --restart unless-stopped %s`+
				` -p 27017:27017 -v xm-mongo-data:/data/db mongo:%s 2>&1`,
			net, version,
		))
		flash = fmt.Sprintf("MongoDB %s started (host: xm-mongodb · no auth)", version)

	case "redis":
		if version == "" {
			version = "7.4"
		}
		cmds = append(cmds, fmt.Sprintf(
			`docker run -d --name xm-redis --restart unless-stopped %s`+
				` -p 6379:6379 -v xm-redis-data:/data`+
				` redis:%s-alpine redis-server --save 60 1 --requirepass %s 2>&1`,
			net, version, rootPass,
		))
		flash = fmt.Sprintf("Redis %s started (password: %s · host: xm-redis)", version, rootPass)

	case "clickhouse":
		if version == "" {
			version = "24.8"
		}
		cmds = append(cmds, fmt.Sprintf(
			`docker run -d --name xm-clickhouse --restart unless-stopped %s`+
				` -p 8123:8123 -p 9000:9000 --ulimit nofile=262144:262144`+
				` -v xm-clickhouse-data:/var/lib/clickhouse`+
				` clickhouse/clickhouse-server:%s 2>&1`,
			net, version,
		))
		flash = fmt.Sprintf("ClickHouse %s started (host: xm-clickhouse)", version)

	default:
		http.Redirect(w, r, "/databases?flash="+urlQueryEscape("unknown db type: "+dbType), http.StatusSeeOther)
		return
	}

	for _, cmd := range cmds {
		res, _ := exec.Run(cmd)
		out := ""
		if res != nil {
			out = res.Stdout + res.Stderr
		}
		if res != nil && res.ExitCode != 0 && !strings.Contains(out, "already in use") {
			flash = "install error: " + out
			http.Redirect(w, r, "/databases?flash="+urlQueryEscape(flash), http.StatusSeeOther)
			return
		}
		if strings.Contains(out, "already in use") {
			flash = dbType + " already running"
		}
	}

	if adminer {
		msg, err := dbmanager.InstallAdminer(exec)
		_ = hostfirewall.Allow([]int{8081}, "tcp")
		if err != nil {
			flash += " · Adminer: " + err.Error()
		} else {
			flash += " · " + msg
		}
	}

	if pgadmin && dbType == "postgres" {
		msg, err := dbmanager.InstallPgAdmin(exec, pgaEmail, pgaPass)
		_ = hostfirewall.Allow([]int{5050}, "tcp")
		if err != nil {
			flash += " · pgAdmin: " + err.Error()
		} else {
			flash += " · " + msg
		}
	}

	dbmanager.EnsureDBToolNetworking(exec)

	if dbName != "" {
		mgr := dbmanager.NewManager(dbmanager.DBType(dbType), exec)
		if mgr != nil {
			var createErr error
			var user string
			for attempt := 0; attempt < 8; attempt++ {
				_, _ = exec.Run("sleep 3")
				user, createErr = dbmanager.ProvisionDatabase(mgr, dbName, dbUser, dbPass)
				if createErr == nil {
					break
				}
			}
			if createErr != nil {
				flash += " · db create failed: " + createErr.Error()
			} else {
				flash += " · database \"" + dbName + "\" created"
				if dbPass != "" && user != "" {
					flash += fmt.Sprintf(" · user %s / %s", user, dbPass)
					_ = h.opts.DB.Create(&storage.DatabaseUser{
						ServerID:  h.localServerID(),
						DBType:    dbType,
						Username:  user,
						Databases: dbName,
						Host:      "%",
					}).Error
				}
			}
		}
	}

	http.Redirect(w, r, "/databases?flash="+urlQueryEscape(flash), http.StatusSeeOther)
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
	sid := h.localServerID()
	res := backup.RunOne(h.localExec(), t, name, backup.DefaultDir)
	backup.Record(h.opts.DB, sid, res, "local")
	flash := "Backup saved"
	if res.Err != nil {
		flash = "Backup failed: " + res.Err.Error()
	} else if res.Filename != "" {
		flash = "Backup saved: " + res.Filename
	}
	http.Redirect(w, r, "/databases?flash="+urlQueryEscape(flash), http.StatusSeeOther)
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

func (h *handler) postNodeDatabaseToolAdminer(w http.ResponseWriter, r *http.Request) {
	msg, err := dbmanager.InstallAdminer(h.localExec())
	_ = hostfirewall.Allow([]int{8081}, "tcp")
	if err != nil {
		http.Redirect(w, r, "/databases?flash="+urlQueryEscape("Adminer: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/databases?flash="+urlQueryEscape(msg), http.StatusSeeOther)
}

func (h *handler) postNodeDatabaseToolPgAdmin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	msg, err := dbmanager.InstallPgAdmin(h.localExec(), strings.TrimSpace(r.FormValue("email")), strings.TrimSpace(r.FormValue("password")))
	_ = hostfirewall.Allow([]int{5050}, "tcp")
	if err != nil {
		http.Redirect(w, r, "/databases?flash="+urlQueryEscape("pgAdmin: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/databases?flash="+urlQueryEscape(msg), http.StatusSeeOther)
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
	case "uptimekuma":
		return uptimekuma.New(db, sid)
	case "databasus":
		return databasus.New(db, sid)
	default:
		return nil
	}
}

func (h *handler) getNodeServices(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	names := []string{
		"registry", "gitea", "rustfs", "rabbitmq", "kafka",
		"mattermost", "bugsink", "umami", "powerdns", "mailinbox", "netdata",
		"uptimekuma", "databasus",
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
	if name == "" {
		http.Redirect(w, r, "/storage?flash="+url.QueryEscape("bucket required"), http.StatusSeeOther)
		return
	}
	cli, _, err := h.rustfsReady(r.Context())
	if err != nil {
		http.Redirect(w, r, "/storage?flash="+url.QueryEscape("RustFS offline"), http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := cli.CreateBucket(ctx, name); err != nil {
		http.Redirect(w, r, "/storage?flash="+url.QueryEscape("Create failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/storage?flash="+url.QueryEscape("Bucket "+name+" created"), http.StatusSeeOther)
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
