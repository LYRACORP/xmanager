package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lyracorp/xmanager/internal/ops"
)

type OpenAI struct {
	name     string
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

func NewOpenAI(cfg ProviderConfig) *OpenAI {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}
	name := cfg.Type
	if name == "" {
		name = "openai"
	}
	return &OpenAI{
		name:     name,
		apiKey:   cfg.APIKey,
		model:    model,
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{},
	}
}

func (o *OpenAI) Name() string { return o.name }

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMsg     `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Tools       []openAIToolDef `json:"tools,omitempty"`
}

type openAIToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

type openAIMsg struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []openAICall  `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
}

type openAICall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content   string       `json:"content"`
			ToolCalls []openAICall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

func (o *OpenAI) Chat(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	options := defaultOptions(opts)
	if options.Model != "" {
		o.model = options.Model
	}

	msgs := make([]openAIMsg, len(messages))
	for i, m := range messages {
		msgs[i] = toOpenAIMsg(m)
	}

	body, _ := json.Marshal(openAIRequest{
		Model:       o.model,
		Messages:    msgs,
		MaxTokens:   options.MaxTokens,
		Temperature: options.Temperature,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", o.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	if o.name == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://github.com/lyracorp/xmanager")
		req.Header.Set("X-Title", "XManager")
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OpenAI API request: %w", err)
	}
	defer resp.Body.Close()

	var result openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("OpenAI API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}

	return result.Choices[0].Message.Content, nil
}

func (o *OpenAI) ChatStream(ctx context.Context, messages []Message, out chan<- string, opts ...Option) error {
	defer close(out)

	options := defaultOptions(opts)
	if options.Model != "" {
		o.model = options.Model
	}

	msgs := make([]openAIMsg, len(messages))
	for i, m := range messages {
		msgs[i] = toOpenAIMsg(m)
	}

	body, _ := json.Marshal(openAIRequest{
		Model:       o.model,
		Messages:    msgs,
		MaxTokens:   options.MaxTokens,
		Temperature: options.Temperature,
		Stream:      true,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", o.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	if o.name == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://github.com/lyracorp/xmanager")
		req.Header.Set("X-Title", "XManager")
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("OpenAI stream request: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				select {
				case out <- choice.Delta.Content:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return scanner.Err()
}

func (o *OpenAI) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.endpoint+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	models := make([]string, len(result.Data))
	for i, m := range result.Data {
		models[i] = m.ID
	}
	return models, nil
}

func toOpenAIMsg(m Message) openAIMsg {
	msg := openAIMsg{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID, Name: m.Name}
	if len(m.ToolCalls) > 0 {
		msg.ToolCalls = make([]openAICall, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			msg.ToolCalls[i].ID = tc.ID
			msg.ToolCalls[i].Type = "function"
			msg.ToolCalls[i].Function.Name = tc.Name
			msg.ToolCalls[i].Function.Arguments = tc.Args
		}
	}
	return msg
}

func (o *OpenAI) ChatWithTools(ctx context.Context, messages []Message, tools []ops.ToolSpec, opts ...Option) (ChatTurn, error) {
	options := defaultOptions(opts)
	model := o.model
	if options.Model != "" {
		model = options.Model
	}
	msgs := make([]openAIMsg, len(messages))
	for i, m := range messages {
		msgs[i] = toOpenAIMsg(m)
	}
	var defs []openAIToolDef
	for _, t := range tools {
		d := openAIToolDef{Type: "function"}
		d.Function.Name = t.Name
		d.Function.Description = t.Description
		d.Function.Parameters = t.InputSchema
		defs = append(defs, d)
	}
	body, _ := json.Marshal(openAIRequest{
		Model:       model,
		Messages:    msgs,
		MaxTokens:   options.MaxTokens,
		Temperature: options.Temperature,
		Tools:       defs,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", o.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatTurn{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	if o.name == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://github.com/lyracorp/xmanager")
		req.Header.Set("X-Title", "XManager")
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return ChatTurn{}, fmt.Errorf("OpenAI API request: %w", err)
	}
	defer resp.Body.Close()
	var result openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ChatTurn{}, fmt.Errorf("decoding response: %w", err)
	}
	if result.Error != nil {
		return ChatTurn{}, fmt.Errorf("OpenAI API error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return ChatTurn{}, fmt.Errorf("no choices returned")
	}
	msg := result.Choices[0].Message
	turn := ChatTurn{Content: msg.Content}
	for _, tc := range msg.ToolCalls {
		turn.ToolCalls = append(turn.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
	}
	return turn, nil
}
