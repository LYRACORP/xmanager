package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/activity"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/logreader"
	"github.com/lyracorp/xmanager/internal/storage"
)

func (h *handler) getNodeLogs(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.basePage(sess, "Logs")
	data.ActiveNav = "logs"
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.populateLogsPage(&data, r)
	h.render(w, "node_logs", data)
}

func (h *handler) getNodeLogsFragment(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.basePage(sess, "Logs")
	h.populateLogsPage(&data, r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "node_logs_body", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *handler) populateLogsPage(data *pageData, r *http.Request) {
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if source == "" || !logreader.IsValidSource(source) {
		source = logreader.SourcePanel
	}
	actor := strings.TrimSpace(r.URL.Query().Get("actor"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rangeKey := strings.TrimSpace(r.URL.Query().Get("range"))
	if rangeKey == "" {
		rangeKey = "7d"
	}
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	container := strings.TrimSpace(r.URL.Query().Get("container"))

	data.LogsSource = source
	data.LogsActor = actor
	data.LogsQuery = q
	data.LogsRange = rangeKey
	data.LogsProject = project
	data.LogsContainer = container
	data.LogsTabSources = logreader.Sources()

	sid := h.localServerID()
	var projects []storage.Project
	_ = h.opts.DB.Where("server_id = ?", sid).Order("updated_at desc").Limit(50).Find(&projects).Error
	data.Projects = projects

	if containers, err := docker.NewManager(h.localExec()).ListContainers(); err == nil {
		data.Containers = containers
	}

	if source == logreader.SourceActivity {
		var since time.Time
		switch rangeKey {
		case "24h":
			since = time.Now().Add(-24 * time.Hour)
		case "7d":
			since = time.Now().Add(-7 * 24 * time.Hour)
		case "all":
		default:
			rangeKey = "7d"
			data.LogsRange = rangeKey
			since = time.Now().Add(-7 * 24 * time.Hour)
		}
		rows, err := activity.List(h.opts.DB, activity.Filter{
			ServerID: sid,
			Source:   strings.TrimSpace(r.URL.Query().Get("asource")),
			Actor:    actor,
			Query:    q,
			Since:    since,
			Limit:    300,
		})
		if err != nil {
			data.Flash = "Load failed: " + err.Error()
		} else {
			data.ActivityLogs = rows
		}
		data.LogsLabel = "Activity audit"
		data.LogsFragmentQS = logsFragmentQuery(data)
		return
	}

	refs := make([]logreader.ProjectRef, 0, len(projects))
	for _, p := range projects {
		refs = append(refs, logreader.ProjectRef{Slug: projectSlug(p.Name), Name: p.Name})
	}
	res, err := logreader.Tail(h.localExec(), source, logreader.Options{
		Lines:     logreader.DefaultLines,
		Project:   projectSlug(project),
		Container: container,
		Projects:  refs,
	})
	if err != nil {
		if data.Flash == "" {
			data.Flash = err.Error()
		}
		data.LogText = ""
		data.LogsLabel = source
		data.LogsFragmentQS = logsFragmentQuery(data)
		return
	}
	data.LogText = res.Text
	data.LogsLabel = res.Label
	data.LogsFragmentQS = logsFragmentQuery(data)
}

func logsFragmentQuery(data *pageData) string {
	v := url.Values{}
	v.Set("source", data.LogsSource)
	if data.LogsProject != "" {
		v.Set("project", data.LogsProject)
	}
	if data.LogsContainer != "" {
		v.Set("container", data.LogsContainer)
	}
	if data.LogsActor != "" {
		v.Set("actor", data.LogsActor)
	}
	if data.LogsQuery != "" {
		v.Set("q", data.LogsQuery)
	}
	if data.LogsRange != "" {
		v.Set("range", data.LogsRange)
	}
	return v.Encode()
}
