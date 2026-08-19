package k8s

import (
	"context"
	"fmt"

	xmk8s "github.com/lyracorp/xmanager/internal/k8s"
	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const serviceType = "k8s"

type K8s struct {
	services.BaseDeployer
	serverID uint
	pool     *ssh.Pool
}

func New(db *gorm.DB, serverID uint) *K8s {
	return &K8s{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (k *K8s) SetPool(p *ssh.Pool) { k.pool = p }

func (k *K8s) Name() string { return serviceType }

func (k *K8s) IsEnabled(exec *ssh.Executor) bool {
	return exec.RunQuiet("kubectl cluster-info >/dev/null 2>&1 && echo yes") == "yes"
}

func (k *K8s) Enable(exec *ssh.Executor, cfg map[string]string) error {
	eng := xmk8s.NewEngine(k.DB, k.pool)
	ver := cfg["k8s_version"]
	if ver == "" {
		ver = cfg["kube_version"]
	}
	var mem storage.K8sClusterMember
	var inv xmk8s.InventorySpec
	var clusterID uint
	if k.DB.Where("server_id = ?", k.serverID).First(&mem).Error == nil {
		c, err := eng.Get(mem.ClusterID)
		if err != nil {
			return err
		}
		inv, err = eng.SpecFromCluster(c)
		if err != nil {
			return err
		}
		clusterID = c.ID
	} else {
		c, got, err := eng.SaveNew(xmk8s.CreateSpec{
			Name:              fmt.Sprintf("server-%d", k.serverID),
			KubeVersion:       ver,
			NetworkPlugin:     cfg["network_plugin"],
			KubesprayImage:    cfg["kubespray_image"],
			Members:           []xmk8s.MemberSpec{{ServerID: k.serverID, Role: "all"}},
			BootstrapServerID: k.serverID,
		})
		if err != nil {
			return err
		}
		inv, clusterID = got, c.ID
	}
	extras := xmk8s.PlaybookExtras(xmk8s.PlaybookCluster, inv.KubeVersion)
	if err := xmk8s.RunOnExecutor(context.Background(), exec, inv, xmk8s.PlaybookCluster, extras, nil); err != nil {
		return err
	}
	return k.SaveInstance(k.serverID, serviceType, "running",
		fmt.Sprintf(`{"k8s_version":"%s","cluster_id":%d}`, inv.KubeVersion, clusterID))
}

func (k *K8s) Disable(exec *ssh.Executor) error {
	eng := xmk8s.NewEngine(k.DB, k.pool)
	var mem storage.K8sClusterMember
	if k.DB.Where("server_id = ?", k.serverID).First(&mem).Error == nil {
		_ = eng.Reset(context.Background(), mem.ClusterID, nil)
	} else {
		inv, _ := xmk8s.Normalize(xmk8s.InventorySpec{
			Hosts: []xmk8s.Host{{Name: "node1", AnsibleHost: "127.0.0.1", AnsibleUser: "root", ControlPlane: true, Etcd: true, Worker: true}},
		})
		_ = xmk8s.RunOnExecutor(context.Background(), exec, inv, xmk8s.PlaybookReset, xmk8s.PlaybookExtras(xmk8s.PlaybookReset, ""), nil)
	}
	return k.SaveInstance(k.serverID, serviceType, "stopped", "")
}

func (k *K8s) Status(exec *ssh.Executor) string {
	if k.IsEnabled(exec) {
		return "running"
	}
	return "stopped"
}
