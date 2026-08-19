package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const maxLogBytes = 256 * 1024

type MemberSpec struct {
	ServerID uint   `json:"server_id"`
	Role     string `json:"role"`
}

type CreateSpec struct {
	Name              string       `json:"name"`
	KubeVersion       string       `json:"kube_version"`
	NetworkPlugin     string       `json:"network_plugin"`
	KubesprayImage    string       `json:"kubespray_image"`
	Members           []MemberSpec `json:"members"`
	BootstrapServerID uint         `json:"bootstrap_server_id"`
}

type Engine struct {
	DB   *gorm.DB
	Pool *ssh.Pool

	local           *ssh.Executor
	runDocker       func(ctx context.Context, exec *ssh.Executor, cmd string, onLine func(string)) error
	fetchAdminConf  func(exec *ssh.Executor) (string, error)
	dockerAvailable func(exec *ssh.Executor) bool

	mu   sync.Mutex
	busy map[uint]struct{}
}

func NewEngine(db *gorm.DB, pool *ssh.Pool) *Engine {
	return &Engine{DB: db, Pool: pool, local: ssh.NewLocalExecutor(), busy: map[uint]struct{}{}}
}

func (e *Engine) localExec() *ssh.Executor {
	if e.local == nil {
		e.local = ssh.NewLocalExecutor()
	}
	return e.local
}

func (e *Engine) List(restrictServerID uint) ([]storage.K8sCluster, error) {
	var list []storage.K8sCluster
	q := e.DB.Order("id desc")
	if restrictServerID > 0 {
		q = q.Where("id IN (?)", e.DB.Model(&storage.K8sClusterMember{}).Select("cluster_id").Where("server_id = ?", restrictServerID))
	}
	err := q.Find(&list).Error
	return list, err
}

func (e *Engine) Get(id uint) (storage.K8sCluster, error) {
	var c storage.K8sCluster
	err := e.DB.First(&c, id).Error
	return c, err
}

func (e *Engine) Members(clusterID uint) ([]storage.K8sClusterMember, error) {
	var m []storage.K8sClusterMember
	err := e.DB.Preload("Server").Where("cluster_id = ?", clusterID).Find(&m).Error
	return m, err
}

func (e *Engine) SpecFromCluster(c storage.K8sCluster) (InventorySpec, error) {
	var spec InventorySpec
	if strings.TrimSpace(c.InventoryJSON) != "" {
		if err := json.Unmarshal([]byte(c.InventoryJSON), &spec); err != nil {
			return spec, err
		}
		for i := range spec.Hosts {
			var srv storage.Server
			if e.DB.First(&srv, spec.Hosts[i].ServerID).Error == nil {
				if spec.Hosts[i].KeyPath == "" {
					spec.Hosts[i].KeyPath = srv.SSHKeyPath
				}
			}
		}
		return Normalize(spec)
	}
	members, err := e.Members(c.ID)
	if err != nil {
		return spec, err
	}
	spec.KubeVersion = c.KubeVersion
	spec.NetworkPlugin = c.NetworkPlugin
	spec.KubesprayImage = c.KubesprayVersion
	for _, m := range members {
		srv := m.Server
		if srv.ID == 0 {
			if err := e.DB.First(&srv, m.ServerID).Error; err != nil {
				return spec, err
			}
		}
		spec.Hosts = append(spec.Hosts, HostFromServer(srv, m.Role))
	}
	return Normalize(spec)
}

func (e *Engine) SaveNew(spec CreateSpec) (storage.K8sCluster, InventorySpec, error) {
	inv, err := e.inventoryFromCreate(spec)
	if err != nil {
		return storage.K8sCluster{}, inv, err
	}
	raw, _ := json.Marshal(inv)
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		name = "cluster"
	}
	c := storage.K8sCluster{
		Name:              name,
		Status:            "pending",
		KubesprayVersion:  inv.KubesprayImage,
		KubeVersion:       inv.KubeVersion,
		NetworkPlugin:     inv.NetworkPlugin,
		InventoryJSON:     string(raw),
		BootstrapServerID: spec.BootstrapServerID,
	}
	if err := e.DB.Create(&c).Error; err != nil {
		return c, inv, err
	}
	for _, h := range inv.Hosts {
		m := storage.K8sClusterMember{
			ClusterID: c.ID,
			ServerID:  h.ServerID,
			Role:      RoleString(h.ControlPlane, h.Etcd, h.Worker),
		}
		if err := e.DB.Create(&m).Error; err != nil {
			return c, inv, err
		}
	}
	return c, inv, nil
}

func (e *Engine) inventoryFromCreate(spec CreateSpec) (InventorySpec, error) {
	inv := InventorySpec{
		KubeVersion:    spec.KubeVersion,
		NetworkPlugin:  spec.NetworkPlugin,
		KubesprayImage: spec.KubesprayImage,
	}
	if len(spec.Members) == 0 {
		return inv, fmt.Errorf("at least one member is required")
	}
	for _, m := range spec.Members {
		var srv storage.Server
		if err := e.DB.First(&srv, m.ServerID).Error; err != nil {
			return inv, fmt.Errorf("server %d: %w", m.ServerID, err)
		}
		role := m.Role
		if role == "" {
			role = "all"
		}
		inv.Hosts = append(inv.Hosts, HostFromServer(srv, role))
	}
	return Normalize(inv)
}

func (e *Engine) Create(ctx context.Context, spec CreateSpec, onLine func(string)) (storage.K8sCluster, error) {
	c, _, err := e.SaveNew(spec)
	if err != nil {
		return c, err
	}
	err = e.RunPlaybook(ctx, c.ID, PlaybookCluster, "", onLine)
	return e.Get(c.ID)
}

func (e *Engine) Scale(ctx context.Context, id uint, members []MemberSpec, onLine func(string)) error {
	c, err := e.Get(id)
	if err != nil {
		return err
	}
	inv, err := e.SpecFromCluster(c)
	if err != nil {
		return err
	}
	existing := map[uint]int{}
	for i, h := range inv.Hosts {
		existing[h.ServerID] = i
	}
	for _, m := range members {
		var srv storage.Server
		if err := e.DB.First(&srv, m.ServerID).Error; err != nil {
			return err
		}
		h := HostFromServer(srv, m.Role)
		if i, ok := existing[m.ServerID]; ok {
			inv.Hosts[i] = h
			_ = e.DB.Model(&storage.K8sClusterMember{}).Where("cluster_id = ? AND server_id = ?", id, m.ServerID).
				Update("role", h.RoleString()).Error
		} else {
			inv.Hosts = append(inv.Hosts, h)
			_ = e.DB.Create(&storage.K8sClusterMember{ClusterID: id, ServerID: m.ServerID, Role: RoleString(h.ControlPlane, h.Etcd, h.Worker)}).Error
		}
	}
	inv, err = Normalize(inv)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(inv)
	_ = e.DB.Model(&c).Update("inventory_json", string(raw)).Error
	return e.RunPlaybook(ctx, id, PlaybookScale, "", onLine)
}

func (h Host) RoleString() string {
	return RoleString(h.ControlPlane, h.Etcd, h.Worker)
}

func (e *Engine) Upgrade(ctx context.Context, id uint, kubeVersion string, onLine func(string)) error {
	if kubeVersion != "" {
		_ = e.DB.Model(&storage.K8sCluster{}).Where("id = ?", id).Updates(map[string]any{
			"kube_version": kubeVersion,
		}).Error
	}
	return e.RunPlaybook(ctx, id, PlaybookUpgrade, kubeVersion, onLine)
}

func (e *Engine) Reset(ctx context.Context, id uint, onLine func(string)) error {
	err := e.RunPlaybook(ctx, id, PlaybookReset, "", onLine)
	_ = e.DB.Model(&storage.K8sCluster{}).Where("id = ?", id).Updates(map[string]any{
		"kubeconfig_enc": "",
		"status":         "pending",
	}).Error
	e.markMembers(id, "stopped")
	return err
}

func (e *Engine) RemoveNode(ctx context.Context, id uint, serverID uint, onLine func(string)) error {
	c, err := e.Get(id)
	if err != nil {
		return err
	}
	inv, err := e.SpecFromCluster(c)
	if err != nil {
		return err
	}
	var names []string
	var kept []Host
	for _, h := range inv.Hosts {
		if h.ServerID == serverID {
			names = append(names, h.Name)
			continue
		}
		kept = append(kept, h)
	}
	if len(names) == 0 {
		return fmt.Errorf("server %d is not a cluster member", serverID)
	}
	inv.Hosts = kept
	raw, _ := json.Marshal(inv)
	_ = e.DB.Model(&c).Update("inventory_json", string(raw)).Error
	_ = e.DB.Where("cluster_id = ? AND server_id = ?", id, serverID).Delete(&storage.K8sClusterMember{}).Error
	extra := "node=" + strings.Join(names, ",")
	return e.RunPlaybook(ctx, id, PlaybookRemove, extra, onLine)
}

func (e *Engine) DeleteRecord(id uint) error {
	_ = e.DB.Where("cluster_id = ?", id).Delete(&storage.K8sClusterMember{}).Error
	return e.DB.Delete(&storage.K8sCluster{}, id).Error
}

func (e *Engine) RunPlaybook(ctx context.Context, clusterID uint, playbook, extra string, onLine func(string)) error {
	if !e.begin(clusterID) {
		return fmt.Errorf("a playbook is already running for this cluster")
	}
	defer e.end(clusterID)

	c, err := e.Get(clusterID)
	if err != nil {
		return err
	}
	inv, err := e.SpecFromCluster(c)
	if err != nil {
		return err
	}
	status := "installing"
	switch playbook {
	case PlaybookReset:
		status = "resetting"
	case PlaybookScale, PlaybookRemove:
		status = "scaling"
	case PlaybookUpgrade:
		status = "upgrading"
	}
	e.setStatus(clusterID, status)
	e.appendLog(clusterID, "starting "+playbook)

	lineFn := func(line string) {
		e.appendLog(clusterID, line)
		if onLine != nil {
			onLine(line)
		}
	}

	extras := PlaybookExtras(playbook, c.KubeVersion)
	if extra != "" && !strings.Contains(extra, "=") {
		// node=name passed as extra for remove-node; PlaybookExtras already has kube_version
	}
	if strings.Contains(extra, "=") {
		extras = append(extras, extra)
	} else if extra != "" && playbook == PlaybookUpgrade {
		extras = PlaybookExtras(playbook, extra)
	}

	user := "root"
	if h, ok := FirstControlPlane(inv); ok {
		user = h.AnsibleUser
	}
	key := SSHKeyPath(inv)
	if key == "" {
		err := fmt.Errorf("no SSH private key path on cluster members (needed to mount into Kubespray)")
		e.fail(clusterID, err)
		return err
	}

	invDir, err := os.MkdirTemp("", "xmanager-kubespray-*")
	if err != nil {
		e.fail(clusterID, err)
		return err
	}
	defer os.RemoveAll(invDir)
	if err := WriteInventoryDir(invDir, inv); err != nil {
		e.fail(clusterID, err)
		return err
	}

	cmd := DockerRunCmd(inv.KubesprayImage, invDir, key, user, NeedsBecome(inv), extras, playbook)
	exec, remoteInv, err := e.pickRunner(c, inv, invDir, key)
	if err != nil {
		e.fail(clusterID, err)
		return err
	}
	if remoteInv != "" {
		cmd = DockerRunCmd(inv.KubesprayImage, remoteInv, remoteInv+"/id_rsa", user, NeedsBecome(inv), extras, playbook)
	}

	runErr := e.dockerStream(ctx, exec, cmd, lineFn)
	if runErr != nil {
		e.fail(clusterID, runErr)
		return runErr
	}

	if playbook == PlaybookCluster || playbook == PlaybookUpgrade {
		if err := e.storeKubeconfig(clusterID, inv); err != nil {
			lineFn("kubeconfig fetch: " + err.Error())
			e.fail(clusterID, err)
			return err
		}
	}
	if playbook == PlaybookReset {
		e.setStatus(clusterID, "pending")
		e.markMembers(clusterID, "stopped")
		return nil
	}
	e.setStatus(clusterID, "ready")
	e.markMembers(clusterID, "running")
	lineFn(playbook + " finished")
	return nil
}

func (e *Engine) hasDocker(exec *ssh.Executor) bool {
	if e.dockerAvailable != nil {
		return e.dockerAvailable(exec)
	}
	return dockerOK(exec.RunQuiet)
}

func (e *Engine) pickRunner(c storage.K8sCluster, inv InventorySpec, localInv, keyPath string) (*ssh.Executor, string, error) {
	local := e.localExec()
	if e.hasDocker(local) {
		return local, "", nil
	}
	boot := c.BootstrapServerID
	if boot == 0 {
		if h, ok := FirstControlPlane(inv); ok {
			boot = h.ServerID
		}
	}
	if boot == 0 {
		return nil, "", fmt.Errorf("docker is not available on this machine; set a bootstrap server with Docker")
	}
	exec, err := e.Executor(boot)
	if err != nil {
		return nil, "", err
	}
	if !e.hasDocker(exec) {
		return nil, "", fmt.Errorf("docker is not available locally or on bootstrap server %d", boot)
	}
	remote := remoteInvDir(c.ID)
	if err := e.uploadInventory(boot, remote, localInv, keyPath); err != nil {
		return nil, "", err
	}
	return exec, remote, nil
}

func (e *Engine) uploadInventory(serverID uint, remoteDir, localInv, keyPath string) error {
	exec, err := e.Executor(serverID)
	if err != nil {
		return err
	}
	if _, err := exec.Run("mkdir -p " + ShellQuote(remoteDir+"/group_vars/k8s_cluster")); err != nil {
		return err
	}
	return e.Pool.WithSFTP(serverID, func(s *ssh.SFTPClient) error {
		hosts, err := os.ReadFile(localInv + "/hosts.ini")
		if err != nil {
			return err
		}
		gv, err := os.ReadFile(localInv + "/group_vars/k8s_cluster/k8s-cluster.yml")
		if err != nil {
			return err
		}
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("reading ssh key: %w", err)
		}
		if err := s.MkdirAll(remoteDir + "/group_vars/k8s_cluster"); err != nil {
			return err
		}
		if err := s.WriteFile(remoteDir+"/hosts.ini", hosts, 0o600); err != nil {
			return err
		}
		if err := s.WriteFile(remoteDir+"/group_vars/k8s_cluster/k8s-cluster.yml", gv, 0o600); err != nil {
			return err
		}
		return s.WriteFile(remoteDir+"/id_rsa", key, 0o600)
	})
}

func (e *Engine) dockerStream(ctx context.Context, exec *ssh.Executor, cmd string, onLine func(string)) error {
	if e.runDocker != nil {
		return e.runDocker(ctx, exec, cmd, onLine)
	}
	return exec.StreamWait(ctx, cmd, onLine)
}

func (e *Engine) Executor(serverID uint) (*ssh.Executor, error) {
	if e.Pool == nil {
		return nil, fmt.Errorf("ssh pool not configured")
	}
	if exec, ok := e.Pool.GetExecutor(serverID); ok {
		if client, cok := e.Pool.GetClient(serverID); cok && client.IsConnected() {
			return exec, nil
		}
	}
	var srv storage.Server
	if err := e.DB.First(&srv, serverID).Error; err != nil {
		return nil, fmt.Errorf("server %d: %w", serverID, err)
	}
	if _, err := e.Pool.Connect(serverID, ssh.ClientConfig{
		Host:     srv.Host,
		Port:     srv.Port,
		User:     srv.User,
		KeyPath:  srv.SSHKeyPath,
		Password: srv.Password,
		JumpHost: srv.JumpHost,
	}); err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", srv.Name, err)
	}
	exec, ok := e.Pool.GetExecutor(serverID)
	if !ok {
		return nil, fmt.Errorf("executor missing after connect")
	}
	return exec, nil
}

func (e *Engine) ControlPlaneExecutor(clusterID uint) (*ssh.Executor, error) {
	c, err := e.Get(clusterID)
	if err != nil {
		return nil, err
	}
	inv, err := e.SpecFromCluster(c)
	if err != nil {
		return nil, err
	}
	h, ok := FirstControlPlane(inv)
	if !ok {
		return nil, fmt.Errorf("no control-plane member")
	}
	return e.Executor(h.ServerID)
}

func (e *Engine) storeKubeconfig(clusterID uint, inv InventorySpec) error {
	var exec *ssh.Executor
	if e.fetchAdminConf == nil {
		h, ok := FirstControlPlane(inv)
		if !ok {
			return fmt.Errorf("no control-plane")
		}
		var err error
		exec, err = e.Executor(h.ServerID)
		if err != nil {
			return err
		}
	}
	raw, err := e.readAdminConf(exec)
	if err != nil {
		return err
	}
	enc, err := config.Encrypt(raw)
	if err != nil {
		return err
	}
	return e.DB.Model(&storage.K8sCluster{}).Where("id = ?", clusterID).Update("kubeconfig_enc", enc).Error
}

func (e *Engine) readAdminConf(exec *ssh.Executor) (string, error) {
	if e.fetchAdminConf != nil {
		return e.fetchAdminConf(exec)
	}
	out := exec.RunQuiet("sudo cat /etc/kubernetes/admin.conf 2>/dev/null || cat /etc/kubernetes/admin.conf")
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("empty /etc/kubernetes/admin.conf")
	}
	return out, nil
}

func (e *Engine) Kubeconfig(clusterID uint) (string, error) {
	c, err := e.Get(clusterID)
	if err != nil {
		return "", err
	}
	if c.KubeconfigEnc == "" {
		return "", fmt.Errorf("no kubeconfig stored")
	}
	return config.Decrypt(c.KubeconfigEnc)
}

func (e *Engine) markMembers(clusterID uint, status string) {
	var members []storage.K8sClusterMember
	_ = e.DB.Where("cluster_id = ?", clusterID).Find(&members).Error
	enabled := status == "running"
	for _, m := range members {
		var inst storage.ServiceInstance
		tx := e.DB.Where("server_id = ? AND service_type = ?", m.ServerID, "k8s").First(&inst)
		if tx.Error != nil {
			inst = storage.ServiceInstance{ServerID: m.ServerID, ServiceType: "k8s"}
		}
		inst.Status = status
		inst.Enabled = enabled
		inst.ConfigJSON = fmt.Sprintf(`{"cluster_id":%d}`, clusterID)
		_ = e.DB.Save(&inst).Error
	}
}

func (e *Engine) begin(id uint) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy == nil {
		e.busy = map[uint]struct{}{}
	}
	if _, ok := e.busy[id]; ok {
		return false
	}
	e.busy[id] = struct{}{}
	return true
}

func (e *Engine) end(id uint) {
	e.mu.Lock()
	delete(e.busy, id)
	e.mu.Unlock()
}

func (e *Engine) setStatus(id uint, status string) {
	_ = e.DB.Model(&storage.K8sCluster{}).Where("id = ?", id).Update("status", status).Error
}

func (e *Engine) fail(id uint, err error) {
	e.appendLog(id, "error: "+err.Error())
	e.setStatus(id, "error")
}

func (e *Engine) appendLog(id uint, line string) {
	var c storage.K8sCluster
	if e.DB.Select("last_log").First(&c, id).Error != nil {
		return
	}
	log := c.LastLog
	if log != "" {
		log += "\n"
	}
	log += line
	if len(log) > maxLogBytes {
		log = log[len(log)-maxLogBytes:]
		if i := strings.IndexByte(log, '\n'); i >= 0 {
			log = log[i+1:]
		}
	}
	_ = e.DB.Model(&storage.K8sCluster{}).Where("id = ?", id).Update("last_log", log).Error
}

// EnableSingle installs a one-node cluster on serverID using remote docker on that host.
func (e *Engine) EnableSingle(ctx context.Context, serverID uint, cfg map[string]string, onLine func(string)) error {
	var existing storage.K8sClusterMember
	if e.DB.Where("server_id = ?", serverID).First(&existing).Error == nil {
		c, _ := e.Get(existing.ClusterID)
		if c.Status == "ready" || c.Status == "installing" {
			return nil
		}
		return e.RunPlaybook(ctx, existing.ClusterID, PlaybookCluster, "", onLine)
	}
	ver := cfg["k8s_version"]
	if ver == "" {
		ver = cfg["kube_version"]
	}
	spec := CreateSpec{
		Name:              fmt.Sprintf("server-%d", serverID),
		KubeVersion:       ver,
		NetworkPlugin:     cfg["network_plugin"],
		KubesprayImage:    cfg["kubespray_image"],
		Members:           []MemberSpec{{ServerID: serverID, Role: "all"}},
		BootstrapServerID: serverID,
	}
	_, err := e.Create(ctx, spec, onLine)
	return err
}
