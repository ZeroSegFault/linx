package providers

import (
	"fmt"

	"github.com/ZeroSegFault/linx/config"
	openai "github.com/sashabaranov/go-openai"
)

// Message represents a chat message in a conversation.
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string // for tool result messages
}

// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON string
}

// ToolDefinition defines a tool the model can call.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]interface{} // JSON Schema
}

// StreamCallback is called with each text chunk during streaming.
type StreamCallback func(chunk string)

// Provider is the interface all LLM providers must implement.
type Provider interface {
	// ChatCompletion sends a system prompt and user message, returning the assistant's response.
	// Kept for backward compatibility.
	ChatCompletion(systemPrompt, userMessage string) (string, error)

	// ChatWithTools sends a conversation with tool definitions and returns the next message.
	ChatWithTools(messages []Message, tools []ToolDefinition) (*Message, error)

	// ChatWithToolsStream is like ChatWithTools but streams text chunks via the callback.
	// The final complete Message is still returned for compatibility.
	ChatWithToolsStream(messages []Message, tools []ToolDefinition, onChunk StreamCallback) (*Message, error)
}

// ModelInfo describes an available model.
type ModelInfo struct {
	ID   string
	Name string
	Size int64 // bytes, 0 if unknown
}

// ModelLister is optionally implemented by providers that can list available models.
type ModelLister interface {
	ListModels() ([]ModelInfo, error)
}

// ToOpenAITools converts ToolDefinitions to OpenAI function tools.
func ToOpenAITools(tools []ToolDefinition) []openai.Tool {
	result := make([]openai.Tool, len(tools))
	for i, t := range tools {
		result[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		}
	}
	return result
}

// ToOpenAIMessages converts our Message type to OpenAI messages.
func ToOpenAIMessages(messages []Message) []openai.ChatCompletionMessage {
	result := make([]openai.ChatCompletionMessage, len(messages))
	for i, m := range messages {
		msg := openai.ChatCompletionMessage{
			Role:    m.Role,
			Content: m.Content,
		}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]openai.ToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				msg.ToolCalls[j] = openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				}
			}
		}
		result[i] = msg
	}
	return result
}

// NewFromConfig creates the appropriate provider based on config.
func NewFromConfig(cfg *config.ProviderConfig) (Provider, error) {
	switch cfg.Type {
	case "openai":
		return NewOpenAI(cfg)
	case "codex":
		return NewCodex(cfg)
	case "ollama":
		return NewOllama(cfg)
	case "anthropic":
		return nil, fmt.Errorf("anthropic provider not yet implemented (planned for v2)")
	default:
		return nil, fmt.Errorf("unknown provider type: %q (supported: openai, codex, ollama, anthropic)", cfg.Type)
	}
}
