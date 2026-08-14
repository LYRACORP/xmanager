package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/activity"
)

func (h *handler) getNodeLogs(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.basePage(sess, "Logs")
	data.ActiveNav = "logs"
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}

	source := strings.TrimSpace(r.URL.Query().Get("source"))
	actor := strings.TrimSpace(r.URL.Query().Get("actor"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rangeKey := strings.TrimSpace(r.URL.Query().Get("range"))
	if rangeKey == "" {
		rangeKey = "7d"
	}

	var since time.Time
	switch rangeKey {
	case "24h":
		since = time.Now().Add(-24 * time.Hour)
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	case "all":
		// zero
	default:
		rangeKey = "7d"
		since = time.Now().Add(-7 * 24 * time.Hour)
	}

	data.LogsSource = source
	data.LogsActor = actor
	data.LogsQuery = q
	data.LogsRange = rangeKey

	rows, err := activity.List(h.opts.DB, activity.Filter{
		ServerID: h.localServerID(),
		Source:   source,
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
	h.render(w, "node_logs", data)
}
