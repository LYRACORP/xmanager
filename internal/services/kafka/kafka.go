package kafka

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "kafka"
const dir = "/opt/xmanager/services/kafka"

type Kafka struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *Kafka {
	return &Kafka{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (k *Kafka) Name() string { return serviceType }

func (k *Kafka) IsEnabled(exec *ssh.Executor) bool {
	return exec.RunQuiet("docker inspect kafka 2>/dev/null | grep -q running && echo yes") == "yes"
}

func (k *Kafka) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "9092"
	}
	host := cfg["host"]
	if host == "" {
		host = "localhost"
	}

	// KRaft mode — no ZooKeeper required (Kafka 3.3+)
	compose := fmt.Sprintf(`services:
  kafka:
    image: bitnami/kafka:latest
    restart: unless-stopped
    environment:
      - KAFKA_CFG_NODE_ID=0
      - KAFKA_CFG_PROCESS_ROLES=controller,broker
      - KAFKA_CFG_LISTENERS=PLAINTEXT://:%s,CONTROLLER://:9093
      - KAFKA_CFG_ADVERTISED_LISTENERS=PLAINTEXT://%s:%s
      - KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT
      - KAFKA_CFG_CONTROLLER_QUORUM_VOTERS=0@kafka:9093
      - KAFKA_CFG_CONTROLLER_LISTENER_NAMES=CONTROLLER
    ports:
      - "%s:9092"
    volumes:
      - kafka_data:/bitnami/kafka
volumes:
  kafka_data:
`, port, host, port, port)

	if err := k.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("kafka enable: %w", err)
	}
	return k.SaveInstance(k.serverID, serviceType, "running",
		fmt.Sprintf(`{"port":"%s","host":"%s"}`, port, host))
}

func (k *Kafka) Disable(exec *ssh.Executor) error {
	if err := k.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("kafka disable: %w", err)
	}
	return k.SaveInstance(k.serverID, serviceType, "stopped", "")
}

func (k *Kafka) Status(exec *ssh.Executor) string {
	out := exec.RunQuiet("docker inspect --format='{{.State.Status}}' kafka 2>/dev/null")
	if out == "" {
		return "stopped"
	}
	return out
}
