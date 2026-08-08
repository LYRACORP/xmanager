package rabbitmq

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "rabbitmq"
const dir = "/opt/xmanager/services/rabbitmq"

type RabbitMQ struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *RabbitMQ {
	return &RabbitMQ{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (r *RabbitMQ) Name() string { return serviceType }

func (r *RabbitMQ) IsEnabled(exec *ssh.Executor) bool {
	return services.ServiceUp(exec, dir, "rabbitmq") || r.InstanceEnabled(r.serverID, serviceType)
}

func (r *RabbitMQ) Enable(exec *ssh.Executor, cfg map[string]string) error {
	amqpPort := cfg["amqp_port"]
	if amqpPort == "" {
		amqpPort = "5672"
	}
	mgmtPort := cfg["management_port"]
	if mgmtPort == "" {
		mgmtPort = "15672"
	}
	user := cfg["user"]
	if user == "" {
		user = "guest"
	}
	password := cfg["password"]
	if password == "" {
		password = "guest"
	}

	compose := fmt.Sprintf(`services:
  rabbitmq:
    image: rabbitmq:3-management-alpine
    container_name: rabbitmq
    restart: unless-stopped
    environment:
      - RABBITMQ_DEFAULT_USER=%s
      - RABBITMQ_DEFAULT_PASS=%s
    ports:
      - "%s:5672"
      - "%s:15672"
    volumes:
      - rabbitmq_data:/var/lib/rabbitmq
volumes:
  rabbitmq_data:
`, user, password, amqpPort, mgmtPort)

	if err := r.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("rabbitmq enable: %w", err)
	}
	return r.SaveInstance(r.serverID, serviceType, "running",
		fmt.Sprintf(`{"amqp_port":"%s","management_port":"%s"}`, amqpPort, mgmtPort))
}

func (r *RabbitMQ) Disable(exec *ssh.Executor) error {
	if err := r.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("rabbitmq disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f rabbitmq 2>/dev/null || true")
	return r.SaveInstance(r.serverID, serviceType, "stopped", "")
}

func (r *RabbitMQ) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "rabbitmq")
}
