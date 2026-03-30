package notify

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

type Alert struct {
	ServerName string
	Service    string
	Title      string
	Message    string
	Severity   Severity
}

type Notifier interface {
	Send(alert Alert) error
	Test() error
	Name() string
}
