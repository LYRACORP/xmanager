package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/apps"
	"github.com/lyracorp/xmanager/internal/gitforge"
	"github.com/lyracorp/xmanager/internal/project"
	"github.com/lyracorp/xmanager/internal/storage"
)

// projectCardView is the display-ready wrapper for a project on the list page.
type projectCardView struct {
	storage.Project
	Branch         string
	Ports          []string
	LastDeployedAt string // human-readable, e.g. "2h ago"
}

func humanAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}

// getAPIProjectStats returns live CPU/mem for a single running container as JSON.
// Called by HTMX on the projects list page.
func (h *handler) getAPIProjectStats(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	p, err := h.loadNodeProject(uint(id))
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.Write([]byte(`{"cpu":"—","mem":"—"}`))
		return
	}
	if p.DeployStatus != project.StatusRunning {
		w.Write([]byte(`{"cpu":"—","mem":"—"}`))
		return
	}
	name := projectSlug(p.Name)
	exec := h.localExec()
	// docker stats format uses docker's own {{}} template syntax — not Go's
	cmd := `docker stats --no-stream --format '{"cpu":"` + "{{.CPUPerc}}" + `","mem":"` + "{{.MemUsage}}" + `"}' ` + name + ` 2>/dev/null || echo '{"cpu":"—","mem":"—"}'`
	out := strings.TrimSpace(exec.RunQuiet(cmd))
	if out == "" {
		out = `{"cpu":"—","mem":"—"}`
	}
	w.Write([]byte(out))
}

func (h *handler) appsLoader() *apps.Loader {
	return apps.DefaultLoader()
}

func projectSlug(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
}

func upstreamFromPorts(ports []string) string {
	for _, p := range ports {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// host:container or just host
		host := p
		if i := strings.Index(p, ":"); i >= 0 {
			host = p[:i]
		}
		host = strings.TrimSpace(host)
		if host != "" && host != "0" {
			return "http://127.0.0.1:" + host
		}
	}
	return "http://127.0.0.1:8080"
}

func parseProjectConfig(raw string) project.Config {
	var cfg project.Config
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg
}

func (h *handler) loadNodeProject(id uint) (*storage.Project, error) {
	var p storage.Project
	err := h.opts.DB.Where("id = ? AND server_id = ?", id, h.localServerID()).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (h *handler) attachProjectDomain(p *storage.Project, domain, upstream string) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return
	}
	if upstream == "" {
		cfg := parseProjectConfig(p.ConfigJSON)
		upstream = upstreamFromPorts(cfg.Ports)
	}
	_, _ = h.connectDomain(domainConnectOpts{
		Domain:    domain,
		Upstream:  upstream,
		ProjectID: p.ID,
	})
	_ = h.opts.DB.Model(p).Update("domain", domain)
	p.Domain = domain
}

func (h *handler) deployProject(p *storage.Project) (string, error) {
	dep := project.NewDeployer(h.localExec(), h.opts.DB)
	res, err := dep.Deploy(p)
	if err != nil {
		msg := err.Error()
		if res != nil && res.Output != "" {
			msg = res.Output
		}
		return msg, err
	}
	if res != nil && res.Output != "" {
		return res.Output, nil
	}
	return "Deployed", nil
}

// --- List ---

func (h *handler) getNodeProjects(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	var projects []storage.Project
	h.opts.DB.Where("server_id = ?", h.localServerID()).Order("updated_at desc").Find(&projects)
	data := h.basePage(sess, "Projects")
	data.ActiveNav = "projects"
	data.Projects = projects
	// build enriched views (branch, ports, last-deployed)
	views := make([]projectCardView, len(projects))
	for i, p := range projects {
		var cfg project.Config
		if p.ConfigJSON != "" {
			_ = json.Unmarshal([]byte(p.ConfigJSON), &cfg)
		}
		var lastDeploy storage.DeployHistory
		h.opts.DB.Where("project_id = ?", p.ID).Order("triggered_at desc").First(&lastDeploy)
		ago := "never"
		if !lastDeploy.TriggeredAt.IsZero() {
			ago = humanAgo(lastDeploy.TriggeredAt)
		}
		views[i] = projectCardView{
			Project:        p,
			Branch:         cfg.Branch,
			Ports:          cfg.Ports,
			LastDeployedAt: ago,
		}
	}
	data.ProjectViews = views
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.render(w, "node_projects", data)
}

// --- Create chooser + type forms ---

func (h *handler) getNodeProjectsNew(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.basePage(sess, "New project")
	data.ActiveNav = "projects"
	data.CreateType = strings.TrimSpace(r.URL.Query().Get("type"))
	data.ProjectTypes = []string{
		"image", "git", "dockerfile", "compose", "push",
		"oneclick", "clone", "scratch", "archive", "function",
	}
	if data.CreateType == "git" || data.CreateType == "clone" {
		data.GitProviders = h.gitProviderViews()
		data.SelectedProvider = gitforge.NormalizeProvider(r.URL.Query().Get("provider"))
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.render(w, "node_projects_new", data)
}

func (h *handler) postNodeProjects(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	typ := strings.TrimSpace(r.FormValue("type"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || typ == "" {
		http.Redirect(w, r, "/projects/new?flash="+urlQueryEscape("name and type required"), http.StatusSeeOther)
		return
	}
	cfg := project.Config{
		Image:             r.FormValue("image"),
		Tag:               r.FormValue("tag"),
		RepoURL:           r.FormValue("repo_url"),
		Branch:            r.FormValue("branch"),
		ComposeYAML:       r.FormValue("compose_yaml"),
		DockerfileContent: r.FormValue("dockerfile"),
		PushedImage:       r.FormValue("pushed_image"),
		ArchivePath:       r.FormValue("archive_path"),
		Runtime:           r.FormValue("runtime"),
		Handler:           r.FormValue("handler"),
		CredID:            parseUintForm(r.FormValue("cred_id")),
	}
	if cfg.CredID == 0 {
		if p := strings.TrimSpace(r.FormValue("provider")); p != "" {
			cfg.CredID = h.connectedCredID(p)
		}
	}
	if (typ == "git" || typ == "clone") && strings.TrimSpace(cfg.RepoURL) == "" {
		http.Redirect(w, r, "/projects/new?type="+url.QueryEscape(typ)+"&flash="+urlQueryEscape("select a repository or paste a URL"), http.StatusSeeOther)
		return
	}
	if ports := strings.TrimSpace(r.FormValue("ports")); ports != "" {
		parts := strings.Split(ports, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		cfg.Ports = parts
	}
	cfgJSON, _ := json.Marshal(cfg)
	secret, _ := project.GenerateWebhookSecret()
	persistent := r.FormValue("persistent_data") == "on" || r.FormValue("persistent_data") == "1"
	source := r.FormValue("source")
	if source == "" {
		source = cfg.RepoURL
		if source == "" {
			source = cfg.Image
		}
		if source == "" {
			source = cfg.PushedImage
		}
		if source == "" && cfg.ComposeYAML != "" {
			source = "compose"
		}
	}
	p := storage.Project{
		Name:           name,
		ServerID:       h.localServerID(),
		Type:           typ,
		Source:         source,
		Domain:         strings.TrimSpace(r.FormValue("domain")),
		PersistentData: persistent,
		DeployStatus:   project.StatusPending,
		ConfigJSON:     string(cfgJSON),
		WebhookSecret:  secret,
	}
	if err := h.opts.DB.Create(&p).Error; err != nil {
		http.Redirect(w, r, "/projects/new?type="+url.QueryEscape(typ)+"&flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if p.Domain != "" {
		h.attachProjectDomain(&p, p.Domain, "")
	}
	deployNow := r.FormValue("deploy_now") == "on" || r.FormValue("deploy_now") == "1"
	if deployNow {
		msg, err := h.deployProject(&p)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/projects/%d?tab=deploy&flash=%s", p.ID, urlQueryEscape("created; deploy failed: "+msg)), http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, fmt.Sprintf("/projects/%d?flash=%s", p.ID, urlQueryEscape("Project created")), http.StatusSeeOther)
}

// --- Templates (CapRover one-click) ---

func (h *handler) getNodeProjectTemplates(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	summaries, err := apps.CachedSummaries()
	data := h.basePage(sess, "One-click apps")
	data.ActiveNav = "projects"
	data.TemplateQuery = q
	if err != nil {
		data.Flash = err.Error()
	}
	if q != "" {
		var filtered []apps.Summary
		for _, s := range summaries {
			hay := strings.ToLower(s.ID + " " + s.DisplayName + " " + s.Description)
			if strings.Contains(hay, q) {
				filtered = append(filtered, s)
			}
		}
		summaries = filtered
	}
	data.TemplateSummaries = summaries
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.render(w, "node_projects_templates", data)
}

func (h *handler) getNodeProjectTemplate(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	id := r.PathValue("id")
	app, err := h.appsLoader().Load(id)
	if err != nil {
		http.Redirect(w, r, "/oneclick?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	data := h.basePage(sess, "Install "+app.DisplayName)
	data.ActiveNav = "projects"
	data.TemplateApp = app
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.render(w, "node_project_template", data)
}

func (h *handler) postNodeProjectTemplate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := r.PathValue("id")
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = id
	}
	values := map[string]string{}
	for k, vals := range r.Form {
		if strings.HasPrefix(k, "var_") && len(vals) > 0 {
			values[strings.TrimPrefix(k, "var_")] = vals[0]
		}
	}
	// Also accept CapRover $$cap_* field names directly
	for k, vals := range r.Form {
		if strings.HasPrefix(k, "$$") && len(vals) > 0 {
			values[k] = vals[0]
		}
	}
	compose, err := h.appsLoader().Render(id, values)
	if err != nil {
		http.Redirect(w, r, "/oneclick/"+url.PathEscape(id)+"?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	// Inject app name into common CapRover placeholders
	compose = strings.ReplaceAll(compose, "$$cap_appname", projectSlug(name))
	cfg := project.Config{ComposeYAML: compose}
	if ports := strings.TrimSpace(r.FormValue("ports")); ports != "" {
		cfg.Ports = strings.Split(ports, ",")
	}
	cfgJSON, _ := json.Marshal(cfg)
	secret, _ := project.GenerateWebhookSecret()
	p := storage.Project{
		Name:          name,
		ServerID:      h.localServerID(),
		Type:          string(project.TypeOneClick),
		Source:        "oneclick:" + id,
		Domain:        strings.TrimSpace(r.FormValue("domain")),
		DeployStatus:  project.StatusPending,
		ConfigJSON:    string(cfgJSON),
		WebhookSecret: secret,
	}
	if err := h.opts.DB.Create(&p).Error; err != nil {
		http.Redirect(w, r, "/oneclick/"+url.PathEscape(id)+"?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if p.Domain != "" {
		h.attachProjectDomain(&p, p.Domain, "")
	}
	deployNow := r.FormValue("deploy_now") != "0" && r.FormValue("deploy_now") != "off"
	if deployNow {
		msg, err := h.deployProject(&p)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/projects/%d?tab=deploy&flash=%s", p.ID, urlQueryEscape("created; deploy failed: "+msg)), http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, fmt.Sprintf("/projects/%d?flash=%s", p.ID, urlQueryEscape("Template installed")), http.StatusSeeOther)
}

// --- Detail ---

func (h *handler) getNodeProjectDetail(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	p, err := h.loadNodeProject(uint(id))
	if err != nil {
		http.Redirect(w, r, "/projects?flash="+urlQueryEscape("project not found"), http.StatusSeeOther)
		return
	}
	tab := strings.TrimSpace(r.URL.Query().Get("tab"))
	if tab == "" {
		tab = "overview"
	}
	cfg := parseProjectConfig(p.ConfigJSON)
	var domains []storage.ProjectDomain
	h.opts.DB.Where("project_id = ?", p.ID).Order("domain asc").Find(&domains)
	var envs []storage.ProjectEnvVar
	h.opts.DB.Where("project_id = ?", p.ID).Order("key asc").Find(&envs)
	var history []storage.DeployHistory
	h.opts.DB.Where("project_id = ?", p.ID).Order("triggered_at desc").Limit(20).Find(&history)

	data := h.basePage(sess, p.Name)
	data.ActiveNav = "projects"
	data.Project = p
	data.ProjectConfig = cfg
	data.ProjectDomainsList = domains
	data.ProjectEnvVars = envs
	data.DeployHistory = history
	data.ActiveTab = tab
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}

	if tab == "logs" {
		data.LogText = h.projectLogs(p)
	}
	h.render(w, "node_project_detail", data)
}

func (h *handler) projectLogs(p *storage.Project) string {
	name := projectSlug(p.Name)
	exec := h.localExec()
	out := exec.RunQuiet(fmt.Sprintf(
		`cd /opt/xmanager/projects/%s 2>/dev/null && docker compose logs --tail=200 2>/dev/null || docker logs --tail=200 %s 2>/dev/null || echo "No logs yet"`,
		name, name,
	))
	return out
}

func (h *handler) postNodeProjectDeploy(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	p, err := h.loadNodeProject(uint(id))
	if err != nil {
		http.Redirect(w, r, "/projects?flash="+urlQueryEscape("not found"), http.StatusSeeOther)
		return
	}
	msg, err := h.deployProject(p)
	flash := "Deployed"
	if err != nil {
		flash = "Deploy failed: " + msg
	} else if msg != "" && msg != "Deployed" {
		flash = "Deployed"
	}
	http.Redirect(w, r, fmt.Sprintf("/projects/%d?tab=deploy&flash=%s", p.ID, urlQueryEscape(flash)), http.StatusSeeOther)
}

func (h *handler) postNodeProjectStop(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	p, err := h.loadNodeProject(uint(id))
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	name := projectSlug(p.Name)
	_, _ = h.localExec().Run(fmt.Sprintf(
		`docker rm -f %s 2>/dev/null; cd /opt/xmanager/projects/%s && docker compose down 2>/dev/null || true`,
		name, name,
	))
	_ = h.opts.DB.Model(p).Update("deploy_status", project.StatusStopped).Error
	http.Redirect(w, r, fmt.Sprintf("/projects/%d?flash=%s", p.ID, urlQueryEscape("Stopped")), http.StatusSeeOther)
}

func (h *handler) postNodeProjectRestart(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	p, err := h.loadNodeProject(uint(id))
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	name := projectSlug(p.Name)
	_, _ = h.localExec().Run(fmt.Sprintf(
		`cd /opt/xmanager/projects/%s 2>/dev/null && docker compose restart 2>/dev/null || docker restart %s 2>/dev/null || true`,
		name, name,
	))
	_ = h.opts.DB.Model(p).Update("deploy_status", project.StatusRunning).Error
	http.Redirect(w, r, fmt.Sprintf("/projects/%d?flash=%s", p.ID, urlQueryEscape("Restarted")), http.StatusSeeOther)
}

func (h *handler) postNodeProjectDomain(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	p, err := h.loadNodeProject(uint(id))
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	domain := strings.TrimSpace(r.FormValue("domain"))
	upstream := strings.TrimSpace(r.FormValue("upstream"))
	h.attachProjectDomain(p, domain, upstream)
	http.Redirect(w, r, fmt.Sprintf("/projects/%d?tab=domains&flash=%s", p.ID, urlQueryEscape("Domain attached")), http.StatusSeeOther)
}

func (h *handler) postNodeProjectEnv(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	p, err := h.loadNodeProject(uint(id))
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	key := strings.TrimSpace(r.FormValue("key"))
	val := r.FormValue("value")
	if key == "" {
		http.Redirect(w, r, fmt.Sprintf("/projects/%d?tab=env&flash=%s", p.ID, urlQueryEscape("key required")), http.StatusSeeOther)
		return
	}
	var existing storage.ProjectEnvVar
	if h.opts.DB.Where("project_id = ? AND key = ?", p.ID, key).First(&existing).Error == nil {
		existing.ValueEncrypted = val
		_ = h.opts.DB.Save(&existing).Error
	} else {
		_ = h.opts.DB.Create(&storage.ProjectEnvVar{
			ProjectID:      p.ID,
			Key:            key,
			ValueEncrypted: val,
		}).Error
	}
	// Mirror into ConfigJSON EnvVars for deploy engine
	cfg := parseProjectConfig(p.ConfigJSON)
	if cfg.EnvVars == nil {
		cfg.EnvVars = map[string]string{}
	}
	cfg.EnvVars[key] = val
	b, _ := json.Marshal(cfg)
	_ = h.opts.DB.Model(p).Update("config_json", string(b)).Error
	http.Redirect(w, r, fmt.Sprintf("/projects/%d?tab=env&flash=%s", p.ID, urlQueryEscape("Env saved")), http.StatusSeeOther)
}

func (h *handler) postNodeProjectEnvDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	envID, _ := strconv.ParseUint(r.PathValue("env_id"), 10, 64)
	p, err := h.loadNodeProject(uint(id))
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	var ev storage.ProjectEnvVar
	if h.opts.DB.Where("id = ? AND project_id = ?", envID, p.ID).First(&ev).Error == nil {
		cfg := parseProjectConfig(p.ConfigJSON)
		if cfg.EnvVars != nil {
			delete(cfg.EnvVars, ev.Key)
			b, _ := json.Marshal(cfg)
			_ = h.opts.DB.Model(p).Update("config_json", string(b)).Error
		}
		h.opts.DB.Delete(&ev)
	}
	http.Redirect(w, r, fmt.Sprintf("/projects/%d?tab=env&flash=%s", p.ID, urlQueryEscape("Env deleted")), http.StatusSeeOther)
}

func (h *handler) postNodeProjectDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	p, err := h.loadNodeProject(uint(id))
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	name := projectSlug(p.Name)
	_, _ = h.localExec().Run(fmt.Sprintf(
		`docker rm -f %s 2>/dev/null; cd /opt/xmanager/projects/%s && docker compose down -v 2>/dev/null || true; rm -rf /opt/xmanager/projects/%s`,
		name, name, name,
	))
	h.opts.DB.Where("project_id = ?", p.ID).Delete(&storage.ProjectDomain{})
	h.opts.DB.Where("project_id = ?", p.ID).Delete(&storage.ProjectEnvVar{})
	h.opts.DB.Where("project_id = ?", p.ID).Delete(&storage.ProjectDatabase{})
	h.opts.DB.Delete(p)
	http.Redirect(w, r, "/projects?flash="+urlQueryEscape("Project deleted"), http.StatusSeeOther)
}
