package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/k8s"
)

func (c *Catalog) k8sEngine() *k8s.Engine {
	return k8s.NewEngine(c.DB, c.Pool)
}

func (c *Catalog) registerK8s() {
	c.add(Tool{
		Name:        "k8s_list_clusters",
		Group:       "k8s",
		Risk:        RiskRead,
		Description: "List Kubespray-managed Kubernetes clusters (id, name, status, versions). Kubeconfig is never returned.",
		InputSchema: objSchema(map[string]any{}),
		Handler:     toolK8sList,
	})
	c.add(Tool{
		Name:        "k8s_create",
		Group:       "k8s",
		Risk:        RiskWrite,
		Description: "Create a cluster record and start Kubespray cluster.yml (Docker on the XManager host, else bootstrap server). members is JSON [{server_id, role}].",
		InputSchema: objSchema(map[string]any{
			"name":                strProp("Cluster name"),
			"kube_version":        strProp("Kubernetes version extra-var, default v1.34.0"),
			"network_plugin":      strProp("CNI, default calico"),
			"kubespray_image":     strProp("quay.io/kubespray/kubespray:v2.31.0"),
			"members":             strProp("JSON array of {server_id, role} where role is all, worker, or control-plane,etcd"),
			"bootstrap_server_id": numProp("Fleet server with Docker if local Docker is missing"),
		}, "name", "members"),
		Handler: toolK8sCreate,
	})
	c.add(Tool{
		Name:        "k8s_kubectl",
		Group:       "k8s",
		Risk:        RiskWrite,
		Description: "Run kubectl on the first control plane over SSH. Pass args as a string array (e.g. [\"get\",\"nodes\"]). drain/delete are destructive — prefer confirmation in the UI.",
		InputSchema: objSchema(map[string]any{
			"cluster_id": numProp("Cluster ID"),
			"args":       arrStrProp("kubectl arguments after kubectl"),
		}, "cluster_id", "args"),
		Handler: toolK8sKubectl,
	})
	c.add(Tool{
		Name:        "k8s_helm",
		Group:       "k8s",
		Risk:        RiskWrite,
		Description: "Helm on the first control plane. op=list|install|uninstall. Installs Helm if missing.",
		InputSchema: objSchema(map[string]any{
			"cluster_id": numProp("Cluster ID"),
			"op":         strProp("list, install, or uninstall"),
			"name":       strProp("Release name"),
			"chart":      strProp("Chart name or URL"),
			"ns":         strProp("Namespace"),
			"values":     strProp("Optional values YAML for install"),
		}, "cluster_id", "op"),
		Handler: toolK8sHelm,
	})
}

func toolK8sList(_ context.Context, c *Catalog, _ map[string]any) (string, error) {
	list, err := c.k8sEngine().List(0)
	if err != nil {
		return "", err
	}
	type row struct {
		ID            uint   `json:"id"`
		Name          string `json:"name"`
		Status        string `json:"status"`
		KubeVersion   string `json:"kube_version"`
		NetworkPlugin string `json:"network_plugin"`
	}
	out := make([]row, 0, len(list))
	for _, cl := range list {
		out = append(out, row{cl.ID, cl.Name, cl.Status, cl.KubeVersion, cl.NetworkPlugin})
	}
	return jsonText(out)
}

func toolK8sCreate(ctx context.Context, c *Catalog, args map[string]any) (string, error) {
	raw := argStr(args, "members")
	var members []k8s.MemberSpec
	if err := json.Unmarshal([]byte(raw), &members); err != nil {
		return "", fmt.Errorf("members must be JSON array: %w", err)
	}
	spec := k8s.CreateSpec{
		Name:              argStr(args, "name"),
		KubeVersion:       argStr(args, "kube_version"),
		NetworkPlugin:     argStr(args, "network_plugin"),
		KubesprayImage:    argStr(args, "kubespray_image"),
		Members:           members,
		BootstrapServerID: argUint(args, "bootstrap_server_id"),
	}
	eng := c.k8sEngine()
	cl, _, err := eng.SaveNew(spec)
	if err != nil {
		return "", err
	}
	go func() { _ = eng.RunPlaybook(context.Background(), cl.ID, k8s.PlaybookCluster, "", nil) }()
	return jsonText(map[string]any{"id": cl.ID, "name": cl.Name, "status": "installing"})
}

func argStringSlice(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			s, _ := it.(string)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	case string:
		return strings.Fields(x)
	}
	return nil
}

func toolK8sKubectl(ctx context.Context, c *Catalog, args map[string]any) (string, error) {
	id := argUint(args, "cluster_id")
	argv := argStringSlice(args, "args")
	if len(argv) == 0 {
		return "", fmt.Errorf("args required")
	}
	return c.k8sEngine().Kubectl(ctx, id, argv...)
}

func toolK8sHelm(ctx context.Context, c *Catalog, args map[string]any) (string, error) {
	id := argUint(args, "cluster_id")
	eng := c.k8sEngine()
	switch strings.ToLower(argStr(args, "op")) {
	case "list":
		return eng.HelmList(ctx, id, argStr(args, "ns"))
	case "install":
		return eng.HelmInstall(ctx, id, argStr(args, "name"), argStr(args, "chart"), argStr(args, "ns"), argStr(args, "values"))
	case "uninstall":
		return eng.HelmUninstall(ctx, id, argStr(args, "name"), argStr(args, "ns"))
	default:
		return "", fmt.Errorf("op must be list, install, or uninstall")
	}
}
