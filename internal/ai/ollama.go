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

type Ollama struct {
	host   string
	model  string
	client *http.Client
}

func NewOllama(cfg ProviderConfig) *Ollama {
	host := cfg.Endpoint
	if host == "" {
		host = "http://localhost:11434"
	}
	model := cfg.Model
	if model == "" {
		model = "llama3"
	}
	return &Ollama{
		host:   strings.TrimRight(host, "/"),
		model:  model,
		client: &http.Client{},
	}
}

func (o *Ollama) Name() string { return "ollama" }

type ollamaRequest struct {
	Model    string       `json:"model"`
	Messages []ollamaMsg  `json:"messages"`
	Stream   bool         `json:"stream"`
	Options  *ollamaOpts  `json:"options,omitempty"`
}

type ollamaMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOpts struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error,omitempty"`
}

func (o *Ollama) Chat(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	options := defaultOptions(opts)
	model := o.model
	if options.Model != "" {
		model = options.Model
	}

	msgs := make([]ollamaMsg, len(messages))
	for i, m := range messages {
		msgs[i] = ollamaMsg{Role: string(m.Role), Content: m.Content}
	}

	body, _ := json.Marshal(ollamaRequest{
		Model:    model,
		Messages: msgs,
		Stream:   false,
		Options:  &ollamaOpts{Temperature: options.Temperature, NumPredict: options.MaxTokens},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", o.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Ollama API request: %w", err)
	}
	defer resp.Body.Close()

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding Ollama response: %w", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("Ollama error: %s", result.Error)
	}

	return result.Message.Content, nil
}

func (o *Ollama) ChatStream(ctx context.Context, messages []Message, out chan<- string, opts ...Option) error {
	defer close(out)

	options := defaultOptions(opts)
	model := o.model
	if options.Model != "" {
		model = options.Model
	}

	msgs := make([]ollamaMsg, len(messages))
	for i, m := range messages {
		msgs[i] = ollamaMsg{Role: string(m.Role), Content: m.Content}
	}

	body, _ := json.Marshal(ollamaRequest{
		Model:    model,
		Messages: msgs,
		Stream:   true,
		Options:  &ollamaOpts{Temperature: options.Temperature, NumPredict: options.MaxTokens},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", o.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("Ollama stream request: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var chunk ollamaResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		if chunk.Message.Content != "" {
			select {
			case out <- chunk.Message.Content:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return scanner.Err()
}

func (o *Ollama) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.host+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing Ollama models: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]string, len(result.Models))
	for i, m := range result.Models {
		models[i] = m.Name
	}
	return models, nil
}
