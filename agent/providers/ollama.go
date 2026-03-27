package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ZeroSegFault/linx/config"
)

// Ollama implements the Provider interface for the native Ollama API.
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllama creates a new Ollama provider.
func NewOllama(cfg *config.ProviderConfig) (*Ollama, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	model := cfg.Model
	if model == "" {
		model = "llama3.2"
	}

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	return &Ollama{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

// -- Ollama API request/response types --

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Tools    []ollamaTool        `json:"tools,omitempty"`
}

type ollamaChatMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content"`
	ToolCalls  []ollamaToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ollamaToolCall struct {
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function ollamaToolCallFunc   `json:"function"`
}

type ollamaToolCallFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ollamaChatResponse struct {
	Message ollamaChatMessage `json:"message"`
	Done    bool              `json:"done"`
	Error   string            `json:"error,omitempty"`
}

type ollamaTagsResponse struct {
	Models []ollamaModel `json:"models"`
}

type ollamaModel struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

// ChatCompletion sends a simple chat request.
func (o *Ollama) ChatCompletion(systemPrompt, userMessage string) (string, error) {
	msgs := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}
	resp, err := o.ChatWithTools(msgs, nil)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// ChatWithTools sends messages with tool definitions and returns the assistant response.
func (o *Ollama) ChatWithTools(messages []Message, tools []ToolDefinition) (*Message, error) {
	req := ollamaChatRequest{
		Model:    o.model,
		Messages: toOllamaMessages(messages),
		Stream:   false,
	}

	if len(tools) > 0 {
		req.Tools = toOllamaTools(tools)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), "POST", o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Ollama API request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama API error (HTTP %d): %s", httpResp.StatusCode, string(respBody))
	}

	var chatResp ollamaChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("parsing Ollama response: %w", err)
	}

	if chatResp.Error != "" {
		return nil, fmt.Errorf("Ollama error: %s", chatResp.Error)
	}

	msg := &Message{
		Role:    chatResp.Message.Role,
		Content: chatResp.Message.Content,
	}

	if len(chatResp.Message.ToolCalls) > 0 {
		msg.ToolCalls = make([]ToolCall, len(chatResp.Message.ToolCalls))
		for i, tc := range chatResp.Message.ToolCalls {
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			argsStr := string(tc.Function.Arguments)
			msg.ToolCalls[i] = ToolCall{
				ID:        id,
				Name:      tc.Function.Name,
				Arguments: argsStr,
			}
		}
	}

	return msg, nil
}

// ChatWithToolsStream falls back to non-streaming for Ollama — emits the full response as one chunk.
func (o *Ollama) ChatWithToolsStream(messages []Message, tools []ToolDefinition, onChunk StreamCallback) (*Message, error) {
	msg, err := o.ChatWithTools(messages, tools)
	if err != nil {
		return nil, err
	}
	if onChunk != nil && msg.Content != "" {
		onChunk(msg.Content)
	}
	return msg, nil
}

// ListModels returns available models from the Ollama instance.
func (o *Ollama) ListModels() ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(context.Background(), "GET", o.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpResp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Ollama API request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama API error (HTTP %d): %s", httpResp.StatusCode, string(body))
	}

	var tags ollamaTagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	models := make([]ModelInfo, len(tags.Models))
	for i, m := range tags.Models {
		models[i] = ModelInfo{
			ID:   m.Name,
			Name: m.Name,
			Size: m.Size,
		}
	}
	return models, nil
}

// -- conversion helpers --

func toOllamaMessages(msgs []Message) []ollamaChatMessage {
	result := make([]ollamaChatMessage, len(msgs))
	for i, m := range msgs {
		om := ollamaChatMessage{
			Role:    m.Role,
			Content: m.Content,
		}
		if m.ToolCallID != "" {
			om.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			om.ToolCalls = make([]ollamaToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				om.ToolCalls[j] = ollamaToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: ollamaToolCallFunc{
						Name:      tc.Name,
						Arguments: json.RawMessage(tc.Arguments),
					},
				}
			}
		}
		result[i] = om
	}
	return result
}

func toOllamaTools(tools []ToolDefinition) []ollamaTool {
	result := make([]ollamaTool, len(tools))
	for i, t := range tools {
		result[i] = ollamaTool{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		}
	}
	return result
}
