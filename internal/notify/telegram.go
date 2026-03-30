package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type Telegram struct {
	BotToken string
	ChatID   string
	client   *http.Client
}

func NewTelegram(botToken, chatID string) *Telegram {
	return &Telegram{
		BotToken: botToken,
		ChatID:   chatID,
		client:   &http.Client{},
	}
}

func (t *Telegram) Name() string { return "telegram" }

func (t *Telegram) Send(alert Alert) error {
	icon := severityIcon(alert.Severity)
	text := fmt.Sprintf("%s *%s*\n\n*Server:* %s\n*Service:* %s\n\n%s",
		icon, alert.Title, alert.ServerName, alert.Service, alert.Message)

	return t.sendMessage(text)
}

func (t *Telegram) Test() error {
	return t.sendMessage("XManager test notification - connection successful!")
}

func (t *Telegram) sendMessage(text string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.BotToken)

	params := url.Values{
		"chat_id":    {t.ChatID},
		"text":       {text},
		"parse_mode": {"Markdown"},
	}

	resp, err := t.client.PostForm(apiURL, params)
	if err != nil {
		return fmt.Errorf("sending Telegram message: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if !result.OK {
		return fmt.Errorf("Telegram API error: %s", result.Description)
	}
	return nil
}

func severityIcon(s Severity) string {
	switch s {
	case SeverityCritical:
		return "🔴"
	case SeverityError:
		return "🟠"
	case SeverityWarning:
		return "🟡"
	default:
		return "🔵"
	}
}
