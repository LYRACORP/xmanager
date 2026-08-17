package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/lyracorp/xmanager/internal/ai"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/ops"
	"github.com/lyracorp/xmanager/internal/recon"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/workflow"
)

func (h *handler) registerAIWorkflows(mux *http.ServeMux, authz, adminz func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /ai", authz(h.getAI))
	mux.HandleFunc("POST /ai/chat", authz(h.postAIChat))
	mux.HandleFunc("POST /ai/transcribe", authz(h.postAITranscribe))
	mux.HandleFunc("POST /settings/ai", adminz(h.postSettingsAI))
	mux.HandleFunc("POST /settings/ai/profile", adminz(h.postSettingsAIProfile))
	mux.HandleFunc("POST /settings/ai/profile/save", adminz(h.postSettingsAIProfileSave))
	mux.HandleFunc("GET /workflows", authz(h.getWorkflows))
	mux.HandleFunc("POST /workflows", adminz(h.postWorkflows))
	mux.HandleFunc("GET /workflows/{id}", authz(h.getWorkflowEdit))
	mux.HandleFunc("POST /workflows/{id}", adminz(h.postWorkflowSave))
	mux.HandleFunc("POST /workflows/{id}/run", adminz(h.postWorkflowRun))
	mux.HandleFunc("POST /workflows/{id}/toggle", adminz(h.postWorkflowToggle))
	mux.HandleFunc("POST /workflows/{id}/delete", adminz(h.postWorkflowDelete))
	mux.HandleFunc("POST /webhook/workflow/{id}", h.postWorkflowWebhook)
}

var aiSessions sync.Map // userID -> *ai.Agent

func (h *handler) getAI(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.baseOrControl(sess, "Assistant")
	data.ActiveNav = "ai"
	if h.opts.Config != nil {
		data.AIProvider = h.opts.Config.AI.Provider
		data.AIModel = h.opts.Config.AI.Model
		data.AIEndpoint = h.opts.Config.AI.Endpoint
	}
	data.AllServers = nil
	if h.opts.DB != nil {
		_ = h.opts.DB.Find(&data.AllServers).Error
	}
	h.render(w, "ai", data)
}

func (h *handler) baseOrControl(sess *session, title string) pageData {
	if h.nodeMode {
		return h.nodePage(sess, title)
	}
	data := pageData{Title: title, Session: sess, NodeMode: false}
	fillPageACL(&data, sess)
	return data
}

func (h *handler) postAIChat(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	msg := strings.TrimSpace(r.FormValue("message"))
	if msg == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}
	sess := sessionFromCtx(r.Context())
	if sess == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	agent, err := h.agentFor(sess, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.workflows != nil {
		h.workflows.TriggerChat(msg)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	ctx := r.Context()
	err = agent.Run(ctx, msg, func(ev ai.Event) {
		payload, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	})
	if err != nil {
		fmt.Fprintf(w, "data: {\"type\":\"error\",\"text\":%q}\n\n", err.Error())
	}
	h.persistAISession(sess, sidFromRequest(r, h), agent)
}

func sidFromRequest(r *http.Request, h *handler) uint {
	if h.nodeMode {
		return h.localServerID()
	}
	n, _ := strconv.ParseUint(r.FormValue("server_id"), 10, 64)
	return uint(n)
}

func (h *handler) persistAISession(sess *session, sid uint, agent *ai.Agent) {
	if h.opts.DB == nil || sess == nil || agent == nil {
		return
	}
	type turn struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var turns []turn
	for _, m := range agent.History() {
		if m.Role == ai.RoleSystem {
			continue
		}
		turns = append(turns, turn{Role: string(m.Role), Content: m.Content})
	}
	raw, err := json.Marshal(turns)
	if err != nil {
		return
	}
	rec := storage.AISession{ServerID: sid, Title: "Web chat", MessagesJSON: string(raw)}
	var existing storage.AISession
	q := h.opts.DB.Where("server_id = ? AND title = ?", sid, "Web chat").Order("id desc")
	if err := q.First(&existing).Error; err == nil {
		existing.MessagesJSON = string(raw)
		_ = h.opts.DB.Save(&existing).Error
		return
	}
	_ = h.opts.DB.Create(&rec).Error
}

func (h *handler) agentFor(sess *session, r *http.Request) (*ai.Agent, error) {
	if h.opts.Config == nil {
		return nil, fmt.Errorf("config missing")
	}
	sid := uint(0)
	if h.nodeMode {
		sid = h.localServerID()
	} else if n, _ := strconv.ParseUint(r.FormValue("server_id"), 10, 64); n > 0 {
		sid = uint(n)
	}
	key := fmt.Sprintf("%d:%d", sess.UserID, sid)
	if v, ok := aiSessions.Load(key); ok {
		return v.(*ai.Agent), nil
	}
	p, err := ai.NewProvider(ai.ProviderConfigFromAI(h.opts.Config.AI))
	if err != nil {
		return nil, err
	}
	cat := h.catalog
	if cat == nil {
		cat = ops.New(h.opts.DB, h.opts.Pool)
	}
	extra := ""
	if sid > 0 {
		sctx := ai.ServerContext{ServerName: fmt.Sprintf("id=%d", sid)}
		if prof, err := recon.GetLatestProfile(h.opts.DB, sid); err == nil && prof != nil {
			sctx.Profile = prof.ProfileJSON
		}
		extra = ai.BuildSystemPrompt(sctx).Content
	}
	ag := ai.NewAgent(p, cat, extra)
	if sid > 0 {
		ag.SetLockedServer(sid)
	}
	if h.opts.DB != nil {
		var rec storage.AISession
		if err := h.opts.DB.Where("server_id = ? AND title = ?", sid, "Web chat").Order("id desc").First(&rec).Error; err == nil {
			var turns []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if json.Unmarshal([]byte(rec.MessagesJSON), &turns) == nil {
				msgs := make([]ai.Message, 0, len(turns))
				for _, t := range turns {
					msgs = append(msgs, ai.Message{Role: ai.Role(t.Role), Content: t.Content})
				}
				ag.LoadHistory(msgs)
			}
		}
	}
	aiSessions.Store(key, ag)
	return ag, nil
}

func (h *handler) postAITranscribe(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, hdr, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "audio required", http.StatusBadRequest)
		return
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := h.opts.Config.AI
	key := strings.TrimSpace(cfg.WhisperKey)
	if key == "" {
		key = cfg.APIKey
	}
	endpoint := cfg.Endpoint
	if cfg.Provider != "openai" && cfg.Provider != "" && !strings.Contains(endpoint, "openai.com") {
		endpoint = "https://api.openai.com/v1"
	}
	name := "audio.webm"
	if hdr != nil && hdr.Filename != "" {
		name = hdr.Filename
	}
	text, err := ai.TranscribeAudio(r.Context(), key, endpoint, name, b)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"text": text})
}

func (h *handler) postSettingsAI(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.opts.Config == nil {
		http.Redirect(w, r, "/settings?flash=no+config", http.StatusSeeOther)
		return
	}
	h.opts.Config.AI.Provider = strings.TrimSpace(r.FormValue("provider"))
	h.opts.Config.AI.Model = strings.TrimSpace(r.FormValue("model"))
	if k := r.FormValue("api_key"); strings.TrimSpace(k) != "" {
		h.opts.Config.AI.APIKey = k
	}
	h.opts.Config.AI.Endpoint = strings.TrimSpace(r.FormValue("endpoint"))
	h.opts.Config.AI.OllamaHost = strings.TrimSpace(r.FormValue("ollama_host"))
	if wk := r.FormValue("whisper_key"); strings.TrimSpace(wk) != "" {
		h.opts.Config.AI.WhisperKey = wk
	}
	_ = config.Save(h.opts.Config)
	h.upsertAIProfile(h.opts.Config.AI, "default", true)
	aiSessions = sync.Map{}
	http.Redirect(w, r, "/settings?flash=AI+settings+saved", http.StatusSeeOther)
}

func (h *handler) upsertAIProfile(cfg config.AIConfig, name string, isDefault bool) {
	if h.opts.DB == nil {
		return
	}
	if name == "" {
		name = "default"
	}
	rec := storage.AIConfigRecord{Name: name, Provider: cfg.Provider, LLMModel: cfg.Model, Endpoint: cfg.Endpoint, IsDefault: isDefault}
	if cfg.APIKey != "" {
		if enc, err := config.Encrypt(cfg.APIKey); err == nil {
			rec.APIKeyEncrypted = enc
		}
	}
	var existing storage.AIConfigRecord
	q := h.opts.DB.Where("name = ?", name)
	if err := q.First(&existing).Error; err == nil {
		existing.Provider = rec.Provider
		existing.LLMModel = rec.LLMModel
		existing.Endpoint = rec.Endpoint
		if rec.APIKeyEncrypted != "" {
			existing.APIKeyEncrypted = rec.APIKeyEncrypted
		}
		if isDefault {
			_ = h.opts.DB.Model(&storage.AIConfigRecord{}).Where("is_default = ?", true).Update("is_default", false).Error
			existing.IsDefault = true
		}
		_ = h.opts.DB.Save(&existing).Error
		return
	}
	if isDefault {
		_ = h.opts.DB.Model(&storage.AIConfigRecord{}).Where("is_default = ?", true).Update("is_default", false).Error
	}
	_ = h.opts.DB.Create(&rec).Error
}

func (h *handler) postSettingsAIProfile(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.FormValue("profile_id"), 10, 64)
	if h.opts.DB == nil || h.opts.Config == nil || id == 0 {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	var rec storage.AIConfigRecord
	if err := h.opts.DB.First(&rec, id).Error; err != nil {
		http.Redirect(w, r, "/settings?flash=profile+not+found", http.StatusSeeOther)
		return
	}
	h.opts.Config.AI.Provider = rec.Provider
	h.opts.Config.AI.Model = rec.LLMModel
	h.opts.Config.AI.Endpoint = rec.Endpoint
	if rec.APIKeyEncrypted != "" {
		if k, err := config.Decrypt(rec.APIKeyEncrypted); err == nil {
			h.opts.Config.AI.APIKey = k
		}
	}
	_ = config.Save(h.opts.Config)
	_ = h.opts.DB.Model(&storage.AIConfigRecord{}).Where("is_default = ?", true).Update("is_default", false).Error
	rec.IsDefault = true
	_ = h.opts.DB.Save(&rec).Error
	aiSessions = sync.Map{}
	http.Redirect(w, r, "/settings?flash=AI+profile+applied", http.StatusSeeOther)
}

func (h *handler) postSettingsAIProfileSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("profile_name"))
	if name == "" {
		name = "extra"
	}
	if h.opts.Config != nil {
		h.upsertAIProfile(h.opts.Config.AI, name, false)
	}
	http.Redirect(w, r, "/settings?flash=AI+profile+saved", http.StatusSeeOther)
}

func (h *handler) getWorkflows(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.baseOrControl(sess, "Workflows")
	data.ActiveNav = "workflows"
	if h.opts.DB != nil {
		_ = h.opts.DB.Order("id desc").Find(&data.Workflows).Error
	}
	h.render(w, "workflows", data)
}

func (h *handler) postWorkflows(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Untitled workflow"
	}
	wf := storage.Workflow{Name: name, Trigger: "manual", GraphJSON: `{"nodes":[],"edges":[]}`}
	_ = h.opts.DB.Create(&wf).Error
	http.Redirect(w, r, fmt.Sprintf("/workflows/%d", wf.ID), http.StatusSeeOther)
}

func (h *handler) loadWF(id uint) (*storage.Workflow, error) {
	var wf storage.Workflow
	if err := h.opts.DB.First(&wf, id).Error; err != nil {
		return nil, err
	}
	return &wf, nil
}

func (h *handler) getWorkflowEdit(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	wf, err := h.loadWF(uint(id))
	if err != nil {
		http.Redirect(w, r, "/workflows", http.StatusSeeOther)
		return
	}
	sess := sessionFromCtx(r.Context())
	data := h.baseOrControl(sess, wf.Name)
	data.ActiveNav = "workflows"
	data.Workflow = wf
	data.WorkflowGraph = wf.GraphJSON
	if h.catalog != nil {
		data.ToolPalette = h.catalog.List()
	}
	if h.opts.DB != nil {
		_ = h.opts.DB.Where("workflow_id = ?", wf.ID).Order("id desc").Limit(20).Find(&data.WorkflowRuns).Error
	}
	h.render(w, "workflow_edit", data)
}

func (h *handler) postWorkflowSave(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	wf, err := h.loadWF(uint(id))
	if err != nil {
		http.Redirect(w, r, "/workflows", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	if n := strings.TrimSpace(r.FormValue("name")); n != "" {
		wf.Name = n
	}
	if g := r.FormValue("graph"); g != "" {
		wf.GraphJSON = g
	}
	wf.Trigger = strings.TrimSpace(r.FormValue("trigger"))
	if wf.Trigger == "" {
		wf.Trigger = "manual"
	}
	wf.CronExpr = strings.TrimSpace(r.FormValue("cron_expr"))
	wf.ChatPhrase = strings.TrimSpace(r.FormValue("chat_phrase"))
	if h.workflows != nil {
		if graph, err := workflow.ParseGraph(wf.GraphJSON); err == nil {
			wf.HasDestructive = h.workflows.HasDestructive(graph)
			if wf.HasDestructive {
				wf.Enabled = false
			}
		}
	}
	_ = h.opts.DB.Save(wf).Error
	http.Redirect(w, r, fmt.Sprintf("/workflows/%d?flash=saved", wf.ID), http.StatusSeeOther)
}

func (h *handler) postWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if h.workflows == nil {
		http.Error(w, "engine missing", 500)
		return
	}
	_, err := h.workflows.Run(context.Background(), uint(id), "manual")
	flash := "ran"
	if err != nil {
		flash = err.Error()
	}
	http.Redirect(w, r, fmt.Sprintf("/workflows/%d?flash=%s", id, urlQueryEscape(flash)), http.StatusSeeOther)
}

func (h *handler) postWorkflowToggle(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	wf, err := h.loadWF(uint(id))
	if err != nil {
		http.Redirect(w, r, "/workflows", http.StatusSeeOther)
		return
	}
	wf.Enabled = !wf.Enabled
	_ = h.opts.DB.Save(wf).Error
	http.Redirect(w, r, "/workflows/"+strconv.FormatUint(id, 10), http.StatusSeeOther)
}

func (h *handler) postWorkflowDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	_ = h.opts.DB.Delete(&storage.Workflow{}, id).Error
	http.Redirect(w, r, "/workflows", http.StatusSeeOther)
}

func (h *handler) postWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if h.workflows == nil {
		http.Error(w, "unavailable", 503)
		return
	}
	var wf storage.Workflow
	if err := h.opts.DB.First(&wf, id).Error; err != nil || (wf.Trigger != "webhook" && wf.Trigger != "manual") {
		http.Error(w, "not found", 404)
		return
	}
	if !wf.Enabled && wf.HasDestructive {
		http.Error(w, "disabled", 403)
		return
	}
	go func() { _, _ = h.workflows.Run(context.Background(), wf.ID, "webhook") }()
	w.WriteHeader(http.StatusAccepted)
}
