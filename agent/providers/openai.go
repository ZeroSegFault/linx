package providers

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ZeroSegFault/linx/config"
	openai "github.com/sashabaranov/go-openai"
)

// OpenAI implements the Provider interface for OpenAI-compatible APIs.
type OpenAI struct {
	client  *openai.Client
	model   string
	timeout time.Duration
}

// NewOpenAI creates a new OpenAI provider.
func NewOpenAI(cfg *config.ProviderConfig) (*OpenAI, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai provider requires api_key in config (set in %s)", config.DefaultPath())
	}

	clientCfg := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		clientCfg.BaseURL = cfg.BaseURL
	}

	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	return &OpenAI{
		client:  openai.NewClientWithConfig(clientCfg),
		model:   model,
		timeout: timeout,
	}, nil
}

// ChatCompletion sends a simple chat completion request and returns the response text.
func (o *OpenAI) ChatCompletion(systemPrompt, userMessage string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()

	resp, err := o.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: o.model,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
				{Role: openai.ChatMessageRoleUser, Content: userMessage},
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("chat completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response choices returned from API")
	}

	return resp.Choices[0].Message.Content, nil
}

// ChatWithTools sends a conversation with tool definitions and returns the next assistant message.
func (o *OpenAI) ChatWithTools(messages []Message, tools []ToolDefinition) (*Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()

	req := openai.ChatCompletionRequest{
		Model:    o.model,
		Messages: ToOpenAIMessages(messages),
	}

	if len(tools) > 0 {
		req.Tools = ToOpenAITools(tools)
	}

	resp, err := o.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("chat completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned from API")
	}

	choice := resp.Choices[0].Message

	msg := &Message{
		Role:    choice.Role,
		Content: choice.Content,
	}

	if len(choice.ToolCalls) > 0 {
		msg.ToolCalls = make([]ToolCall, len(choice.ToolCalls))
		for i, tc := range choice.ToolCalls {
			msg.ToolCalls[i] = ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}
		}
	}

	return msg, nil
}

// ChatWithToolsStream sends a conversation with tool definitions and streams text chunks.
func (o *OpenAI) ChatWithToolsStream(messages []Message, tools []ToolDefinition, onChunk StreamCallback) (*Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()

	req := openai.ChatCompletionRequest{
		Model:    o.model,
		Messages: ToOpenAIMessages(messages),
		Stream:   true,
	}

	if len(tools) > 0 {
		req.Tools = ToOpenAITools(tools)
	}

	stream, err := o.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("chat completion stream failed: %w", err)
	}
	defer stream.Close()

	var contentBuilder strings.Builder
	var role string
	// Accumulate tool calls by index
	toolCallMap := make(map[int]*openai.ToolCall)

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" || err == io.EOF {
				break
			}
			return nil, fmt.Errorf("stream recv error: %w", err)
		}

		if len(resp.Choices) == 0 {
			continue
		}

		delta := resp.Choices[0].Delta
		if delta.Role != "" {
			role = delta.Role
		}

		// Stream text content
		if delta.Content != "" {
			contentBuilder.WriteString(delta.Content)
			if onChunk != nil {
				onChunk(delta.Content)
			}
		}

		// Accumulate tool calls
		for _, tc := range delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			existing, ok := toolCallMap[idx]
			if !ok {
				existing = &openai.ToolCall{}
				toolCallMap[idx] = existing
			}
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Type != "" {
				existing.Type = tc.Type
			}
			if tc.Function.Name != "" {
				existing.Function.Name = tc.Function.Name
			}
			existing.Function.Arguments += tc.Function.Arguments
		}

		// Check for finish
		if resp.Choices[0].FinishReason != "" {
			break
		}
	}

	if role == "" {
		role = "assistant"
	}

	msg := &Message{
		Role:    role,
		Content: contentBuilder.String(),
	}

	if len(toolCallMap) > 0 {
		// Sort by index to maintain order
		msg.ToolCalls = make([]ToolCall, 0, len(toolCallMap))
		for i := 0; i < len(toolCallMap); i++ {
			if tc, ok := toolCallMap[i]; ok {
				msg.ToolCalls = append(msg.ToolCalls, ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				})
			}
		}
	}

	return msg, nil
}

// ListModels returns available models from the OpenAI-compatible API.
func (o *OpenAI) ListModels() ([]ModelInfo, error) {
	resp, err := o.client.ListModels(context.Background())
	if err != nil {
		return nil, fmt.Errorf("listing models: %w", err)
	}

	models := make([]ModelInfo, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = ModelInfo{
			ID:   m.ID,
			Name: m.ID,
		}
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
}
