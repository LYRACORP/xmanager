package k8s

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type ResourceRow struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Ready     string `json:"ready"`
	Status    string `json:"status"`
	Extra     string `json:"extra"`
}

func (e *Engine) Kubectl(ctx context.Context, clusterID uint, args ...string) (string, error) {
	exec, err := e.ControlPlaneExecutor(clusterID)
	if err != nil {
		return "", err
	}
	return kubectl(exec, args...)
}

func kubectl(exec *ssh.Executor, args ...string) (string, error) {
	cmd := "kubectl " + shellJoin(args)
	res, err := exec.Run(cmd)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout)
	if res.Stderr != "" {
		if out != "" {
			out += "\n"
		}
		out += res.Stderr
	}
	if res.ExitCode != 0 {
		return out, fmt.Errorf("kubectl exit %d: %s", res.ExitCode, truncate(out, 800))
	}
	return out, nil
}

func (e *Engine) Overview(ctx context.Context, clusterID uint) (map[string]any, error) {
	c, err := e.Get(clusterID)
	if err != nil {
		return nil, err
	}
	ver, _ := e.Kubectl(ctx, clusterID, "version", "-o", "json")
	nodes, _ := e.Kubectl(ctx, clusterID, "get", "nodes", "--no-headers")
	health, _ := e.Kubectl(ctx, clusterID, "cluster-info")
	nReady, nTotal := countNodeReady(nodes)
	return map[string]any{
		"name":           c.Name,
		"status":         c.Status,
		"kube_version":   c.KubeVersion,
		"network_plugin": c.NetworkPlugin,
		"kubespray":      c.KubesprayVersion,
		"nodes_ready":    nReady,
		"nodes_total":    nTotal,
		"cluster_info":   truncate(health, 2000),
		"version_json":   ver,
	}, nil
}

func countNodeReady(out string) (ready, total int) {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		total++
		if strings.Contains(line, " Ready") || strings.HasPrefix(fields(line, 1), "Ready") {
			ready++
		}
	}
	return
}

func fields(line string, i int) string {
	p := strings.Fields(line)
	if i < 0 || i >= len(p) {
		return ""
	}
	return p[i]
}

func (e *Engine) GetResources(ctx context.Context, clusterID uint, kind string, allNS bool) ([]ResourceRow, error) {
	args := []string{"get", kind, "-o", "json"}
	if allNS {
		args = append(args, "-A")
	}
	raw, err := e.Kubectl(ctx, clusterID, args...)
	if err != nil {
		return nil, err
	}
	return parseResourceList(raw, kind)
}

func parseResourceList(raw, defaultKind string) ([]ResourceRow, error) {
	var doc struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("parsing kubectl json: %w", err)
	}
	rows := make([]ResourceRow, 0, len(doc.Items))
	for _, item := range doc.Items {
		var obj map[string]any
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		rows = append(rows, rowFromObj(obj, defaultKind))
	}
	return rows, nil
}

func rowFromObj(obj map[string]any, defaultKind string) ResourceRow {
	kind, _ := obj["kind"].(string)
	if kind == "" {
		kind = defaultKind
	}
	meta, _ := obj["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	ns, _ := meta["namespace"].(string)
	status, _ := obj["status"].(map[string]any)
	spec, _ := obj["spec"].(map[string]any)
	r := ResourceRow{Namespace: ns, Name: name, Kind: kind}
	switch strings.ToLower(kind) {
	case "node":
		r.Status = nodeCondition(status)
		if info, ok := status["nodeInfo"].(map[string]any); ok {
			r.Extra, _ = info["kubeletVersion"].(string)
		}
	case "namespace":
		if ph, ok := status["phase"].(string); ok {
			r.Status = ph
		}
	case "pod":
		r.Status, _ = status["phase"].(string)
		r.Ready = podReady(status)
	case "service":
		r.Extra, _ = spec["type"].(string)
		r.Status = serviceClusterIP(spec)
	case "secret":
		r.Extra, _ = obj["type"].(string)
		r.Status = "masked"
	case "configmap":
		if data, ok := obj["data"].(map[string]any); ok {
			r.Extra = fmt.Sprintf("%d keys", len(data))
		}
	case "persistentvolumeclaim", "persistentvolume":
		r.Status, _ = status["phase"].(string)
	case "storageclass":
		r.Extra, _ = obj["provisioner"].(string)
	case "deployment", "statefulset", "daemonset", "replicaset":
		r.Ready = replicaReady(status)
		r.Status = r.Ready
	case "job":
		if s, ok := status["succeeded"].(float64); ok {
			r.Status = fmt.Sprintf("succeeded=%d", int(s))
		}
	case "cronjob":
		if spec != nil {
			r.Extra, _ = spec["schedule"].(string)
		}
	case "ingress":
		r.Extra = ingressHosts(spec)
	case "event":
		r.Status, _ = obj["type"].(string)
		r.Extra, _ = obj["reason"].(string)
		if involved, ok := obj["involvedObject"].(map[string]any); ok {
			r.Kind, _ = involved["kind"].(string)
			r.Name, _ = involved["name"].(string)
			r.Namespace, _ = involved["namespace"].(string)
		}
		if msg, ok := obj["message"].(string); ok && r.Extra == "" {
			r.Extra = truncate(msg, 80)
		}
	}
	return r
}

func nodeCondition(status map[string]any) string {
	conds, _ := status["conditions"].([]any)
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m["type"] == "Ready" {
			if m["status"] == "True" {
				return "Ready"
			}
			return "NotReady"
		}
	}
	return ""
}

func podReady(status map[string]any) string {
	conds, _ := status["conditions"].([]any)
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m["type"] == "Ready" {
			if m["status"] == "True" {
				return "1/1"
			}
			return "0/1"
		}
	}
	return ""
}

func replicaReady(status map[string]any) string {
	ready := asInt(status["readyReplicas"])
	want := asInt(status["replicas"])
	return fmt.Sprintf("%d/%d", ready, want)
}

func asInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	}
	return 0
}

func serviceClusterIP(spec map[string]any) string {
	s, _ := spec["clusterIP"].(string)
	return s
}

func ingressHosts(spec map[string]any) string {
	rules, _ := spec["rules"].([]any)
	var hosts []string
	for _, r := range rules {
		m, _ := r.(map[string]any)
		h, _ := m["host"].(string)
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	return strings.Join(hosts, ",")
}

func (e *Engine) Describe(ctx context.Context, clusterID uint, kind, ns, name string) (string, error) {
	args := []string{"describe", kind, name}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	return e.Kubectl(ctx, clusterID, args...)
}

func (e *Engine) Logs(ctx context.Context, clusterID uint, ns, pod, container string, tail int) (string, error) {
	args := []string{"logs", pod}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	if container != "" {
		args = append(args, "-c", container)
	}
	if tail <= 0 {
		tail = 200
	}
	args = append(args, "--tail", fmt.Sprintf("%d", tail))
	return e.Kubectl(ctx, clusterID, args...)
}

func (e *Engine) ScaleWorkload(ctx context.Context, clusterID uint, kind, ns, name string, replicas int) error {
	args := []string{"scale", kind, name, fmt.Sprintf("--replicas=%d", replicas)}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	_, err := e.Kubectl(ctx, clusterID, args...)
	return err
}

func (e *Engine) RolloutRestart(ctx context.Context, clusterID uint, kind, ns, name string) error {
	args := []string{"rollout", "restart", kind + "/" + name}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	_, err := e.Kubectl(ctx, clusterID, args...)
	return err
}

func (e *Engine) DeleteResource(ctx context.Context, clusterID uint, kind, ns, name string) error {
	args := []string{"delete", kind, name}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	_, err := e.Kubectl(ctx, clusterID, args...)
	return err
}

func (e *Engine) NodeAction(ctx context.Context, clusterID uint, action, node string) error {
	switch action {
	case "cordon":
		_, err := e.Kubectl(ctx, clusterID, "cordon", node)
		return err
	case "uncordon":
		_, err := e.Kubectl(ctx, clusterID, "uncordon", node)
		return err
	case "drain":
		_, err := e.Kubectl(ctx, clusterID, "drain", node, "--ignore-daemonsets", "--delete-emptydir-data", "--force")
		return err
	default:
		return fmt.Errorf("unknown node action %q", action)
	}
}

func (e *Engine) ApplyJSON(ctx context.Context, clusterID uint, manifest string) error {
	exec, err := e.ControlPlaneExecutor(clusterID)
	if err != nil {
		return err
	}
	res, err := exec.Run("cat <<'XM_K8S_EOF' | kubectl apply -f -\n" + manifest + "\nXM_K8S_EOF")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("kubectl apply: %s", res.Stderr+res.Stdout)
	}
	return nil
}

func MaskSecretJSON(raw string) string {
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return raw
	}
	maskObj(doc)
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return raw
	}
	return string(b)
}

func maskObj(v any) {
	switch x := v.(type) {
	case map[string]any:
		kind, _ := x["kind"].(string)
		if strings.EqualFold(kind, "secret") || strings.EqualFold(kind, "Secret") {
			if data, ok := x["data"].(map[string]any); ok {
				for k := range data {
					data[k] = "***"
				}
			}
			if str, ok := x["stringData"].(map[string]any); ok {
				for k := range str {
					str[k] = "***"
				}
			}
		}
		if items, ok := x["items"].([]any); ok {
			for _, it := range items {
				maskObj(it)
			}
		}
		for k, child := range x {
			if k == "data" || k == "stringData" {
				continue
			}
			maskObj(child)
		}
	case []any:
		for _, it := range x {
			maskObj(it)
		}
	}
}

func (e *Engine) GetSecretsMasked(ctx context.Context, clusterID uint) ([]ResourceRow, string, error) {
	raw, err := e.Kubectl(ctx, clusterID, "get", "secrets", "-A", "-o", "json")
	if err != nil {
		return nil, "", err
	}
	masked := MaskSecretJSON(raw)
	rows, err := parseResourceList(masked, "Secret")
	return rows, masked, err
}

func shellJoin(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if a == "" || strings.ContainsAny(a, " \t\n'\"$") {
			b.WriteString(ShellQuote(a))
		} else {
			b.WriteString(a)
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
