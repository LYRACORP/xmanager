package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Anthropic struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

func NewAnthropic(cfg ProviderConfig) *Anthropic {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://api.anthropic.com"
	}
	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &Anthropic{
		apiKey:   cfg.APIKey,
		model:    model,
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{},
	}
}

func (a *Anthropic) Name() string { return "anthropic" }

type anthropicRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Messages    []anthropicMsg  `json:"messages"`
	System      string          `json:"system,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (a *Anthropic) Chat(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	options := defaultOptions(opts)
	if options.Model != "" {
		a.model = options.Model
	}

	system := ""
	var msgs []anthropicMsg
	for _, m := range messages {
		if m.Role == RoleSystem {
			system = m.Content
			continue
		}
		msgs = append(msgs, anthropicMsg{Role: string(m.Role), Content: m.Content})
	}

	body, _ := json.Marshal(anthropicRequest{
		Model:     a.model,
		MaxTokens: options.MaxTokens,
		Messages:  msgs,
		System:    system,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", a.endpoint+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Anthropic API request: %w", err)
	}
	defer resp.Body.Close()

	var result anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("Anthropic API error: %s", result.Error.Message)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("no content returned")
	}

	return result.Content[0].Text, nil
}

func (a *Anthropic) ChatStream(ctx context.Context, messages []Message, out chan<- string, opts ...Option) error {
	defer close(out)

	options := defaultOptions(opts)
	if options.Model != "" {
		a.model = options.Model
	}

	system := ""
	var msgs []anthropicMsg
	for _, m := range messages {
		if m.Role == RoleSystem {
			system = m.Content
			continue
		}
		msgs = append(msgs, anthropicMsg{Role: string(m.Role), Content: m.Content})
	}

	body, _ := json.Marshal(anthropicRequest{
		Model:     a.model,
		MaxTokens: options.MaxTokens,
		Messages:  msgs,
		System:    system,
		Stream:    true,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", a.endpoint+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("Anthropic stream request: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Type == "content_block_delta" && event.Delta.Text != "" {
			select {
			case out <- event.Delta.Text:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if event.Type == "message_stop" {
			break
		}
	}

	return scanner.Err()
}

func (a *Anthropic) ListModels(_ context.Context) ([]string, error) {
	return []string{
		"claude-sonnet-4-20250514",
		"claude-opus-4-20250514",
		"claude-3-5-haiku-20241022",
	}, nil
}
