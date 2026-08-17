package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/ssh"
)

func (h *handler) getNodeDatabaseDetail(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	dbType := dbmanager.DBType(r.PathValue("type"))
	name := r.PathValue("name")
	if name == "" {
		http.Redirect(w, r, "/databases?flash="+urlQueryEscape("database name required"), http.StatusSeeOther)
		return
	}
	if !h.canAccessDatabase(r, string(dbType), name) {
		http.NotFound(w, r)
		return
	}

	exec := h.localExec()
	mgr := dbmanager.NewManager(dbType, exec)
	if mgr == nil || !mgr.IsAvailable() {
		http.Redirect(w, r, "/databases?flash="+urlQueryEscape("engine not available"), http.StatusSeeOther)
		return
	}

	detail, err := dbmanager.DatabaseInfo(dbType, exec, name)
	if err != nil {
		http.Redirect(w, r, "/databases?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	pubHost := publicHost(r)
	adminerURL := fmt.Sprintf("http://%s:%s", pubHost, dbmanager.AdminerPort)
	pgAdminURL := fmt.Sprintf("http://%s:%s", pubHost, dbmanager.PgAdminPort)
	containerName := dbmanager.ContainerName(dbType)

	engine := nodeDBView{
		Type:       string(dbType),
		Container:  containerName,
		Host:       containerName,
		Port:       dbmanager.DefaultPort(dbType),
		AdminerURL: adminerURL,
		AdminerOn:  dbmanager.ContainerRunning(exec, dbmanager.AdminerName),
		PgAdminURL: pgAdminURL,
		PgAdminOn:  dbmanager.ContainerRunning(exec, dbmanager.PgAdminName),
	}
	if c, ok := h.dockerContainerByName(exec, containerName); ok {
		engine.Image = c.Image
		engine.Ports = c.Ports
		engine.ContainerID = c.ID
		engine.Running = c.State == "running"
		if p := dbmanager.ParseHostPort(c.Ports); p != "" {
			engine.Port = p
		}
		if c.ID != "" {
			engine.LogsURL = "/docker/" + c.ID + "/logs"
		}
	}

	data := h.basePage(sess, name)
	data.ActiveNav = "databases"
	data.DBDetail = nodeDBDetailView{
		Type:        string(dbType),
		Name:        detail.Name,
		Owner:       detail.Owner,
		Size:        detail.Size,
		Connections: detail.Connections,
		Tables:      detail.Tables,
		Engine:      engine,
		AdminerURL:  dbmanager.AdminerURL(adminerURL, string(dbType), containerName, name, adminerUser(dbType)),
		PgAdminURL:  pgAdminURL,
		PgAdminOn:   engine.PgAdminOn,
	}
	h.render(w, "node_database_detail", data)
}

func (h *handler) getAPIDatabaseEngineStats(w http.ResponseWriter, r *http.Request) {
	dbType := dbmanager.DBType(r.PathValue("type"))
	containerName := dbmanager.ContainerName(dbType)
	w.Header().Set("Content-Type", "application/json")
	if containerName == "" {
		w.Write([]byte(`{"cpu":"—","mem":"—"}`))
		return
	}
	stats := docker.NewManager(h.localExec()).Stats([]string{containerName})
	if st, ok := stats[containerName]; ok {
		b, _ := json.Marshal(map[string]string{"cpu": st.CPUPct, "mem": st.MemUsage})
		w.Write(b)
		return
	}
	w.Write([]byte(`{"cpu":"—","mem":"—"}`))
}

func (h *handler) getAPIDatabaseDetailMetrics(w http.ResponseWriter, r *http.Request) {
	dbType := dbmanager.DBType(r.PathValue("type"))
	name := r.PathValue("name")
	if !h.canAccessDatabase(r, string(dbType), name) {
		http.NotFound(w, r)
		return
	}
	exec := h.localExec()

	detail, err := dbmanager.DatabaseInfo(dbType, exec, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	containerName := dbmanager.ContainerName(dbType)
	stats := docker.NewManager(exec).Stats([]string{containerName})
	cpu, mem := "—", "—"
	if st, ok := stats[containerName]; ok {
		cpu, mem = st.CPUPct, st.MemUsage
	}

	data := h.basePage(sessionFromCtx(r.Context()), name)
	data.DBDetail = nodeDBDetailView{
		Type:        string(dbType),
		Name:        detail.Name,
		Owner:       detail.Owner,
		Size:        detail.Size,
		Connections: detail.Connections,
		Tables:      detail.Tables,
		EngineCPUPct: cpu,
		EngineMem:    mem,
	}
	h.render(w, "node_database_metrics", data)
}

func (h *handler) dockerContainerByName(exec *ssh.Executor, name string) (docker.Container, bool) {
	list, err := docker.NewManager(exec).ListContainers()
	if err != nil {
		return docker.Container{}, false
	}
	for _, c := range list {
		if c.Name == name {
			return c, true
		}
	}
	return docker.Container{}, false
}

func adminerUser(dbType dbmanager.DBType) string {
	switch dbType {
	case dbmanager.PostgreSQL:
		return "postgres"
	case dbmanager.MySQL, dbmanager.MariaDB:
		return "root"
	default:
		return ""
	}
}

func databaseDetailPath(dbType, name string) string {
	return "/databases/" + url.PathEscape(dbType) + "/" + url.PathEscape(name)
}
