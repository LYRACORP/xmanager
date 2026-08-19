package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/k8s"
	"github.com/lyracorp/xmanager/internal/storage"
)

type k8sMemberView struct {
	ServerID uint
	Name     string
	Host     string
	Role     string
}

type k8sResourceRow struct {
	Namespace string
	Name      string
	Kind      string
	Ready     string
	Status    string
	Extra     string
}

func (h *handler) k8sEngine() *k8s.Engine {
	return k8s.NewEngine(h.opts.DB, h.opts.Pool)
}

func (h *handler) k8sScope() uint {
	if h.nodeMode {
		return h.localServerID()
	}
	return 0
}

func (h *handler) canSeeCluster(id uint) bool {
	if !h.nodeMode {
		return true
	}
	var n int64
	h.opts.DB.Model(&storage.K8sClusterMember{}).Where("cluster_id = ? AND server_id = ?", id, h.localServerID()).Count(&n)
	return n > 0
}

func (h *handler) registerK8s(mux *http.ServeMux, authz, adminz func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /k8s", authz(h.getK8s))
	mux.HandleFunc("POST /k8s", adminz(h.postK8sCreate))
	mux.HandleFunc("GET /k8s/{id}", authz(h.getK8sCluster))
	mux.HandleFunc("GET /k8s/{id}/log", authz(h.getK8sLog))
	mux.HandleFunc("POST /k8s/{id}/playbook", adminz(h.postK8sPlaybook))
	mux.HandleFunc("POST /k8s/{id}/reset", adminz(h.postK8sReset))
	mux.HandleFunc("POST /k8s/{id}/action", adminz(h.postK8sAction))
	mux.HandleFunc("POST /k8s/{id}/helm", adminz(h.postK8sHelm))
	mux.HandleFunc("POST /k8s/{id}/delete", adminz(h.postK8sDelete))
}

func (h *handler) getK8s(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.baseOrControl(sess, "Kubernetes")
	data.ActiveNav = "k8s"
	list, _ := h.k8sEngine().List(h.k8sScope())
	data.K8sClusters = list
	if h.opts.DB != nil {
		_ = h.opts.DB.Find(&data.AllServers).Error
	}
	if h.nodeMode && len(data.AllServers) == 0 {
		var srv storage.Server
		if h.opts.DB.First(&srv, h.localServerID()).Error == nil {
			data.AllServers = []storage.Server{srv}
		}
	}
	h.render(w, "k8s", data)
}

func (h *handler) postK8sCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	eng := h.k8sEngine()
	spec := k8s.CreateSpec{
		Name:           strings.TrimSpace(r.FormValue("name")),
		KubeVersion:    strings.TrimSpace(r.FormValue("kube_version")),
		NetworkPlugin:  strings.TrimSpace(r.FormValue("network_plugin")),
		KubesprayImage: strings.TrimSpace(r.FormValue("kubespray_image")),
	}
	if spec.KubeVersion == "" {
		spec.KubeVersion = k8s.DefaultKubeVersion
	}
	if spec.NetworkPlugin == "" {
		spec.NetworkPlugin = k8s.DefaultNetworkPlugin
	}
	if h.nodeMode {
		spec.Members = []k8s.MemberSpec{{ServerID: h.localServerID(), Role: "all"}}
		spec.BootstrapServerID = h.localServerID()
	} else {
		for _, srv := range r.Form["server_id"] {
			id64, _ := strconv.ParseUint(srv, 10, 64)
			if id64 == 0 {
				continue
			}
			role := strings.TrimSpace(r.FormValue("role_" + srv))
			if role == "" || role == "off" {
				continue
			}
			spec.Members = append(spec.Members, k8s.MemberSpec{ServerID: uint(id64), Role: role})
		}
	}
	c, _, err := eng.SaveNew(spec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	go func() { _ = eng.RunPlaybook(context.Background(), c.ID, k8s.PlaybookCluster, "", nil) }()
	http.Redirect(w, r, fmt.Sprintf("/k8s/%d?tab=install", c.ID), http.StatusSeeOther)
}

func (h *handler) parseClusterID(r *http.Request) uint {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	return uint(id)
}

func (h *handler) getK8sCluster(w http.ResponseWriter, r *http.Request) {
	id := h.parseClusterID(r)
	if id == 0 || !h.canSeeCluster(id) {
		http.NotFound(w, r)
		return
	}
	sess := sessionFromCtx(r.Context())
	data := h.baseOrControl(sess, "Kubernetes")
	data.ActiveNav = "k8s"
	eng := h.k8sEngine()
	c, err := eng.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data.K8sCluster = &c
	members, _ := eng.Members(id)
	for _, m := range members {
		name, host := m.Server.Name, m.Server.Host
		data.K8sMembers = append(data.K8sMembers, k8sMemberView{ServerID: m.ServerID, Name: name, Host: host, Role: m.Role})
	}
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "overview"
	}
	data.K8sTab = tab
	data.K8sText = c.LastLog
	ctx := r.Context()
	if c.Status == "ready" {
		h.fillK8sTab(&data, eng, id, tab, ctx)
	}
	h.render(w, "k8s_cluster", data)
}

func (h *handler) fillK8sTab(data *pageData, eng *k8s.Engine, id uint, tab string, ctx context.Context) {
	switch tab {
	case "overview":
		ov, err := eng.Overview(ctx, id)
		if err != nil {
			data.Flash = err.Error()
			return
		}
		data.K8sOverview = ov
	case "nodes":
		h.loadK8sKind(data, eng, ctx, id, "nodes", false)
	case "namespaces":
		h.loadK8sKind(data, eng, ctx, id, "namespaces", false)
	case "workloads":
		h.loadK8sKind(data, eng, ctx, id, "deployments,statefulsets,daemonsets,jobs,cronjobs,pods", true)
	case "network":
		h.loadK8sKind(data, eng, ctx, id, "svc,ingress", true)
	case "config":
		rows, _, err := eng.GetSecretsMasked(ctx, id)
		cms, err2 := eng.GetResources(ctx, id, "configmaps", true)
		if err2 == nil {
			rows = append(cms, rows...)
		}
		h.setK8sRows(data, rows, err)
	case "storage":
		h.loadK8sKind(data, eng, ctx, id, "pvc,pv,sc", true)
	case "events":
		h.loadK8sKind(data, eng, ctx, id, "events", true)
	case "logs":
		data.K8sText = ""
	case "helm":
		out, err := eng.HelmList(ctx, id, "all")
		data.K8sText = out
		if err != nil {
			data.Flash = err.Error()
		}
	case "install":
		// last_log already set
	}
}

func (h *handler) loadK8sKind(data *pageData, eng *k8s.Engine, ctx context.Context, id uint, kind string, allNS bool) {
	rows, err := eng.GetResources(ctx, id, kind, allNS)
	h.setK8sRows(data, rows, err)
}

func (h *handler) setK8sRows(data *pageData, rows []k8s.ResourceRow, err error) {
	if err != nil {
		data.Flash = err.Error()
		return
	}
	for _, r := range rows {
		data.K8sRows = append(data.K8sRows, k8sResourceRow{
			Namespace: r.Namespace, Name: r.Name, Kind: r.Kind, Ready: r.Ready, Status: r.Status, Extra: r.Extra,
		})
	}
}

func (h *handler) getK8sLog(w http.ResponseWriter, r *http.Request) {
	id := h.parseClusterID(r)
	if id == 0 || !h.canSeeCluster(id) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	eng := h.k8sEngine()
	var last string
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			c, err := eng.Get(id)
			if err != nil {
				return
			}
			if c.LastLog != last {
				last = c.LastLog
				fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(last, "\n", "\ndata: "))
				if flusher != nil {
					flusher.Flush()
				}
			}
			switch c.Status {
			case "ready", "error":
				fmt.Fprintf(w, "event: done\ndata: %s\n\n", c.Status)
				if flusher != nil {
					flusher.Flush()
				}
				return
			case "pending":
				if last != "" && !strings.Contains(strings.ToLower(c.LastLog), "starting ") {
					fmt.Fprintf(w, "event: done\ndata: %s\n\n", c.Status)
					if flusher != nil {
						flusher.Flush()
					}
					return
				}
			}
		}
	}
}

func (h *handler) postK8sPlaybook(w http.ResponseWriter, r *http.Request) {
	id := h.parseClusterID(r)
	if id == 0 || !h.canSeeCluster(id) {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	play := r.FormValue("playbook")
	extra := r.FormValue("extra")
	switch play {
	case k8s.PlaybookCluster, k8s.PlaybookScale, k8s.PlaybookUpgrade, k8s.PlaybookRemove:
	default:
		http.Error(w, "unknown playbook", http.StatusBadRequest)
		return
	}
	eng := h.k8sEngine()
	go func() { _ = eng.RunPlaybook(context.Background(), id, play, extra, nil) }()
	http.Redirect(w, r, fmt.Sprintf("/k8s/%d?tab=install", id), http.StatusSeeOther)
}

func (h *handler) postK8sReset(w http.ResponseWriter, r *http.Request) {
	id := h.parseClusterID(r)
	if id == 0 || !h.canSeeCluster(id) {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	if r.FormValue("confirm") != "RESET" {
		http.Error(w, "type RESET to confirm", http.StatusBadRequest)
		return
	}
	eng := h.k8sEngine()
	go func() { _ = eng.Reset(context.Background(), id, nil) }()
	http.Redirect(w, r, fmt.Sprintf("/k8s/%d?tab=install", id), http.StatusSeeOther)
}

func (h *handler) postK8sDelete(w http.ResponseWriter, r *http.Request) {
	id := h.parseClusterID(r)
	if id == 0 || !h.canSeeCluster(id) {
		http.NotFound(w, r)
		return
	}
	_ = h.k8sEngine().DeleteRecord(id)
	http.Redirect(w, r, "/k8s", http.StatusSeeOther)
}

func (h *handler) postK8sAction(w http.ResponseWriter, r *http.Request) {
	id := h.parseClusterID(r)
	if id == 0 || !h.canSeeCluster(id) {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	eng := h.k8sEngine()
	ctx := r.Context()
	action := r.FormValue("action")
	kind := r.FormValue("kind")
	ns := r.FormValue("ns")
	name := r.FormValue("name")
	var err error
	switch action {
	case "cordon", "uncordon", "drain":
		if action == "drain" && r.FormValue("confirm") != "yes" {
			http.Error(w, "confirm drain", http.StatusBadRequest)
			return
		}
		err = eng.NodeAction(ctx, id, action, name)
	case "scale":
		n, _ := strconv.Atoi(r.FormValue("replicas"))
		err = eng.ScaleWorkload(ctx, id, kind, ns, name, n)
	case "restart":
		err = eng.RolloutRestart(ctx, id, kind, ns, name)
	case "delete":
		if r.FormValue("confirm") != "yes" {
			http.Error(w, "confirm delete", http.StatusBadRequest)
			return
		}
		err = eng.DeleteResource(ctx, id, kind, ns, name)
	case "logs":
		out, e := eng.Logs(ctx, id, ns, name, r.FormValue("container"), 200)
		tab := r.FormValue("tab")
		if tab == "" {
			tab = "logs"
		}
		if e != nil {
			http.Redirect(w, r, fmt.Sprintf("/k8s/%d?tab=%s&err=%s", id, tab, e.Error()), http.StatusSeeOther)
			return
		}
		sess := sessionFromCtx(r.Context())
		data := h.baseOrControl(sess, "Kubernetes")
		data.ActiveNav = "k8s"
		c, _ := eng.Get(id)
		data.K8sCluster = &c
		data.K8sTab = "logs"
		data.K8sText = out
		h.render(w, "k8s_cluster", data)
		return
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tab := r.FormValue("tab")
	if tab == "" {
		tab = "overview"
	}
	http.Redirect(w, r, fmt.Sprintf("/k8s/%d?tab=%s", id, tab), http.StatusSeeOther)
}

func (h *handler) postK8sHelm(w http.ResponseWriter, r *http.Request) {
	id := h.parseClusterID(r)
	if id == 0 || !h.canSeeCluster(id) {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	eng := h.k8sEngine()
	ctx := r.Context()
	op := r.FormValue("op")
	name := r.FormValue("name")
	ns := r.FormValue("ns")
	var err error
	switch op {
	case "install":
		_, err = eng.HelmInstall(ctx, id, name, r.FormValue("chart"), ns, r.FormValue("values"))
	case "uninstall":
		if r.FormValue("confirm") != "yes" {
			http.Error(w, "confirm uninstall", http.StatusBadRequest)
			return
		}
		_, err = eng.HelmUninstall(ctx, id, name, ns)
	default:
		http.Error(w, "unknown helm op", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/k8s/%d?tab=helm", id), http.StatusSeeOther)
}
