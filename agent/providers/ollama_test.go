package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZeroSegFault/linx/config"
)

func TestNewOllamaDefaults(t *testing.T) {
	o, err := NewOllama(&config.ProviderConfig{Type: "ollama"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.baseURL != "http://localhost:11434" {
		t.Errorf("expected default base URL, got %s", o.baseURL)
	}
	if o.model != "llama3.2" {
		t.Errorf("expected default model llama3.2, got %s", o.model)
	}
}

func TestOllamaChatWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		resp := ollamaChatResponse{
			Message: ollamaChatMessage{
				Role:    "assistant",
				Content: "Hello from Ollama!",
			},
			Done: true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	o, _ := NewOllama(&config.ProviderConfig{
		Type:    "ollama",
		BaseURL: server.URL,
		Model:   "test-model",
	})

	msgs := []Message{{Role: "user", Content: "hi"}}
	resp, err := o.ChatWithTools(msgs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Ollama!" {
		t.Errorf("expected 'Hello from Ollama!', got %q", resp.Content)
	}
}

func TestOllamaChatWithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaChatResponse{
			Message: ollamaChatMessage{
				Role:    "assistant",
				Content: "",
				ToolCalls: []ollamaToolCall{
					{
						Function: ollamaToolCallFunc{
							Name:      "get_os_info",
							Arguments: json.RawMessage(`{}`),
						},
					},
				},
			},
			Done: true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	o, _ := NewOllama(&config.ProviderConfig{
		Type:    "ollama",
		BaseURL: server.URL,
		Model:   "test-model",
	})

	msgs := []Message{{Role: "user", Content: "what os?"}}
	resp, err := o.ChatWithTools(msgs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_os_info" {
		t.Errorf("expected tool call 'get_os_info', got %q", resp.ToolCalls[0].Name)
	}
}

func TestOllamaListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := ollamaTagsResponse{
			Models: []ollamaModel{
				{Name: "llama3.2:latest", Size: 4_000_000_000},
				{Name: "mistral:7b", Size: 7_000_000_000},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	o, _ := NewOllama(&config.ProviderConfig{BaseURL: server.URL})

	models, err := o.ListModels()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "llama3.2:latest" {
		t.Errorf("expected llama3.2:latest, got %s", models[0].ID)
	}
}

func TestOllamaAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	o, _ := NewOllama(&config.ProviderConfig{BaseURL: server.URL})

	_, err := o.ChatWithTools([]Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestToOllamaMessages(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello", ToolCalls: []ToolCall{{ID: "1", Name: "test", Arguments: "{}"}}},
		{Role: "tool", Content: "result", ToolCallID: "1"},
	}

	result := toOllamaMessages(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[2].ToolCalls[0].Function.Name != "test" {
		t.Error("tool call not preserved")
	}
	if result[3].ToolCallID != "1" {
		t.Error("tool call ID not preserved")
	}
}
