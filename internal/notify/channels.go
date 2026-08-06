package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

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

type MultiNotifier struct {
	notifiers []Notifier
}

func NewMulti(notifiers ...Notifier) *MultiNotifier {
	var active []Notifier
	for _, n := range notifiers {
		if n != nil {
			active = append(active, n)
		}
	}
	return &MultiNotifier{notifiers: active}
}

func (m *MultiNotifier) Send(alert Alert) error {
	var errs []string
	for _, n := range m.notifiers {
		if err := n.Send(alert); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", n.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("notify: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *MultiNotifier) Test() error {
	return m.Send(Alert{
		Title:    "XManager test alert",
		Message:  "Notification channel is working",
		Severity: SeverityInfo,
	})
}

func (m *MultiNotifier) Name() string { return "multi" }

// Webhook notifier

type Webhook struct {
	URL     string
	Headers map[string]string
	client  *http.Client
}

func NewWebhook(url string, headers map[string]string) *Webhook {
	return &Webhook{
		URL:     url,
		Headers: headers,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (w *Webhook) Name() string { return "webhook" }

func (w *Webhook) Send(alert Alert) error {
	payload := map[string]interface{}{
		"server":   alert.ServerName,
		"service":  alert.Service,
		"title":    alert.Title,
		"message":  alert.Message,
		"severity": alert.Severity,
		"time":     time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling webhook payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.Headers {
		req.Header.Set(k, v)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func (w *Webhook) Test() error {
	return w.Send(Alert{Title: "XManager webhook test", Message: "ok", Severity: SeverityInfo})
}

// Email notifier

type Email struct {
	SMTPHost string
	SMTPPort int
	Username string
	Password string
	From     string
	To       []string
}

func NewEmail(host string, port int, username, password, from string, to []string) *Email {
	return &Email{
		SMTPHost: host,
		SMTPPort: port,
		Username: username,
		Password: password,
		From:     from,
		To:       to,
	}
}

func (e *Email) Name() string { return "email" }

func (e *Email) Send(alert Alert) error {
	if e.SMTPHost == "" || len(e.To) == 0 {
		return fmt.Errorf("email not configured")
	}
	subject := fmt.Sprintf("[%s] %s", strings.ToUpper(string(alert.Severity)), alert.Title)
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\nServer: %s\nService: %s\n\n%s\n",
		e.From, strings.Join(e.To, ","), subject, alert.ServerName, alert.Service, alert.Message)

	addr := fmt.Sprintf("%s:%d", e.SMTPHost, e.SMTPPort)
	var auth smtp.Auth
	if e.Username != "" {
		auth = smtp.PlainAuth("", e.Username, e.Password, e.SMTPHost)
	}
	if err := smtp.SendMail(addr, auth, e.From, e.To, []byte(body)); err != nil {
		return fmt.Errorf("sending email: %w", err)
	}
	return nil
}

func (e *Email) Test() error {
	return e.Send(Alert{Title: "XManager email test", Message: "ok", Severity: SeverityInfo})
}

// SMS notifier (generic webhook-based SMS gateway)

type SMS struct {
	WebhookURL string
	APIKey     string
	To         string
	client     *http.Client
}

func NewSMS(webhookURL, apiKey, to string) *SMS {
	return &SMS{
		WebhookURL: webhookURL,
		APIKey:     apiKey,
		To:         to,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *SMS) Name() string { return "sms" }

func (s *SMS) Send(alert Alert) error {
	if s.WebhookURL == "" {
		return fmt.Errorf("sms webhook not configured")
	}
	payload := map[string]string{
		"to":      s.To,
		"message": fmt.Sprintf("[%s] %s: %s", alert.Severity, alert.Title, alert.Message),
		"api_key": s.APIKey,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, s.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending sms: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("sms gateway returned status %d", resp.StatusCode)
	}
	return nil
}

func (s *SMS) Test() error {
	return s.Send(Alert{Title: "XManager SMS test", Message: "ok", Severity: SeverityInfo})
}
