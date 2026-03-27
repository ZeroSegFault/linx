package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ZeroSegFault/linx/agent/providers"
	"github.com/ZeroSegFault/linx/agent/tools"
	"github.com/ZeroSegFault/linx/config"
)

// mockProvider implements providers.Provider for testing.
type mockProvider struct {
	responses []*providers.Message
	callIdx   int
}

func (m *mockProvider) ChatCompletion(system, user string) (string, error) {
	return "mock response", nil
}

func (m *mockProvider) ChatWithTools(msgs []providers.Message, toolDefs []providers.ToolDefinition) (*providers.Message, error) {
	if m.callIdx >= len(m.responses) {
		return &providers.Message{Role: "assistant", Content: "no more responses"}, nil
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

func (m *mockProvider) ChatWithToolsStream(msgs []providers.Message, toolDefs []providers.ToolDefinition, onChunk providers.StreamCallback) (*providers.Message, error) {
	msg, err := m.ChatWithTools(msgs, toolDefs)
	if err != nil {
		return nil, err
	}
	if onChunk != nil && msg.Content != "" {
		onChunk(msg.Content)
	}
	return msg, nil
}

// NewWithProvider creates an Agent with an injected provider for testing.
func NewWithProvider(provider providers.Provider, cfg *config.Config, confirm tools.ConfirmFunc, callback Callback) *Agent {
	confirmFn := confirm
	if !cfg.Behavior.ConfirmDestructive {
		confirmFn = nil
	}
	return &Agent{
		provider: provider,
		tools:    tools.NewRegistry(confirmFn, &cfg.Tools, cfg.Behavior.EnableManpages, cfg.Behavior.AutoBackup),
		callback: callback,
		cfg:      cfg,
	}
}

func defaultTestConfig() *config.Config {
	return &config.Config{
		Provider: config.ProviderConfig{
			Type:  "openai",
			Model: "test-model",
		},
		Behavior: config.BehaviorConfig{
			ConfirmDestructive: false,
			RequireResearch:    false,
		},
	}
}

// collectEvents returns a callback that records events.
func collectEvents() (Callback, *[]Event) {
	var events []Event
	cb := func(e Event) {
		events = append(events, e)
	}
	return cb, &events
}

func TestChatSimpleResponse(t *testing.T) {
	mock := &mockProvider{
		responses: []*providers.Message{
			{Role: "assistant", Content: "Hello, I'm Linx!"},
		},
	}
	cfg := defaultTestConfig()
	cb, _ := collectEvents()

	a := NewWithProvider(mock, cfg, nil, cb)
	result, err := a.Chat("hi")
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}
	if result != "Hello, I'm Linx!" {
		t.Errorf("expected 'Hello, I'm Linx!', got %q", result)
	}
}

func TestChatWithToolCall(t *testing.T) {
	mock := &mockProvider{
		responses: []*providers.Message{
			{
				Role: "assistant",
				ToolCalls: []providers.ToolCall{
					{ID: "call_1", Name: "get_os_info", Arguments: "{}"},
				},
			},
			{Role: "assistant", Content: "You're running Linux!"},
		},
	}
	cfg := defaultTestConfig()
	cb, events := collectEvents()

	a := NewWithProvider(mock, cfg, nil, cb)
	result, err := a.Chat("what OS am I running?")
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}
	if result != "You're running Linux!" {
		t.Errorf("expected 'You're running Linux!', got %q", result)
	}

	// Check that a tool call event was emitted
	foundToolCall := false
	for _, e := range *events {
		if e.Type == EventToolCall {
			foundToolCall = true
			break
		}
	}
	if !foundToolCall {
		t.Error("expected EventToolCall event, none found")
	}
}

func TestResearchGateBlocks(t *testing.T) {
	// First: install_package without research → should be blocked
	// Second: get_os_info (research) → should execute
	// Third: install_package again → should execute (research done)
	// Fourth: final text answer
	mock := &mockProvider{
		responses: []*providers.Message{
			// Round 1: try install_package (blocked)
			{
				Role: "assistant",
				ToolCalls: []providers.ToolCall{
					{ID: "call_1", Name: "install_package", Arguments: `{"packages":"nginx"}`},
				},
			},
			// Round 2: do research
			{
				Role: "assistant",
				ToolCalls: []providers.ToolCall{
					{ID: "call_2", Name: "get_os_info", Arguments: "{}"},
				},
			},
			// Round 3: try install_package again (should work now)
			{
				Role: "assistant",
				ToolCalls: []providers.ToolCall{
					{ID: "call_3", Name: "install_package", Arguments: `{"packages":"nginx"}`},
				},
			},
			// Round 4: final answer
			{Role: "assistant", Content: "Nginx installed!"},
		},
	}
	cfg := defaultTestConfig()
	cfg.Behavior.RequireResearch = true
	cb, events := collectEvents()

	a := NewWithProvider(mock, cfg, nil, cb)
	result, err := a.Chat("install nginx")
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}
	if result != "Nginx installed!" {
		t.Errorf("expected 'Nginx installed!', got %q", result)
	}

	// Check that the first tool result contained the research warning
	toolResults := []Event{}
	for _, e := range *events {
		if e.Type == EventToolResult {
			toolResults = append(toolResults, e)
		}
	}
	if len(toolResults) < 1 {
		t.Fatal("expected at least one tool result event")
	}
	if !strings.Contains(toolResults[0].Data, "Research required") {
		t.Errorf("first tool result should contain 'Research required', got %q", toolResults[0].Data)
	}
}

func TestResearchGateDisabled(t *testing.T) {
	mock := &mockProvider{
		responses: []*providers.Message{
			{
				Role: "assistant",
				ToolCalls: []providers.ToolCall{
					{ID: "call_1", Name: "install_package", Arguments: `{"packages":"nginx"}`},
				},
			},
			{Role: "assistant", Content: "Done!"},
		},
	}
	cfg := defaultTestConfig()
	cfg.Behavior.RequireResearch = false
	cb, events := collectEvents()

	a := NewWithProvider(mock, cfg, nil, cb)
	result, err := a.Chat("install nginx")
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}
	if result != "Done!" {
		t.Errorf("expected 'Done!', got %q", result)
	}

	// Verify no tool result contains "Research required"
	for _, e := range *events {
		if e.Type == EventToolResult && strings.Contains(e.Data, "Research required") {
			t.Error("research gate should be disabled, but got research warning")
		}
	}
}

func TestChatTurnPersistsHistory(t *testing.T) {
	// Mock returns different responses based on call count
	mock := &mockProvider{
		responses: []*providers.Message{
			{Role: "assistant", Content: "Installed hyprland!"},
			{Role: "assistant", Content: "Configured waybar!"},
		},
	}
	cfg := defaultTestConfig()
	cb, _ := collectEvents()

	a := NewWithProvider(mock, cfg, nil, cb)

	// First turn
	resp1, turn1, err := a.ChatTurn("install hyprland")
	if err != nil {
		t.Fatalf("ChatTurn 1 error: %v", err)
	}
	if resp1 != "Installed hyprland!" {
		t.Errorf("turn 1: got %q", resp1)
	}
	if turn1.Number != 1 {
		t.Errorf("expected turn number 1, got %d", turn1.Number)
	}

	// Second turn — should have history from first
	resp2, turn2, err := a.ChatTurn("configure waybar")
	if err != nil {
		t.Fatalf("ChatTurn 2 error: %v", err)
	}
	if resp2 != "Configured waybar!" {
		t.Errorf("turn 2: got %q", resp2)
	}
	if turn2.Number != 2 {
		t.Errorf("expected turn number 2, got %d", turn2.Number)
	}

	// Verify messages accumulated (system + user1 + assistant1 + user2 + assistant2)
	if len(a.Messages()) != 5 {
		t.Errorf("expected 5 messages, got %d", len(a.Messages()))
	}
}

func TestChatTurnResearchGateResetsPerTurn(t *testing.T) {
	mock := &mockProvider{
		responses: []*providers.Message{
			// Turn 1: research tool then destructive — should work
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "1", Name: "get_os_info", Arguments: "{}"},
			}},
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "2", Name: "install_package", Arguments: `{"packages":"nginx"}`},
			}},
			{Role: "assistant", Content: "Installed nginx!"},
			// Turn 2: try destructive without research — should be blocked
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "3", Name: "install_package", Arguments: `{"packages":"htop"}`},
			}},
			// After block, do research
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "4", Name: "get_os_info", Arguments: "{}"},
			}},
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "5", Name: "install_package", Arguments: `{"packages":"htop"}`},
			}},
			{Role: "assistant", Content: "Installed htop!"},
		},
	}

	cfg := defaultTestConfig()
	cfg.Behavior.RequireResearch = true
	cb, events := collectEvents()

	a := NewWithProvider(mock, cfg, nil, cb)

	// Turn 1 — should succeed (research then install)
	_, _, err := a.ChatTurn("install nginx")
	if err != nil {
		t.Fatalf("Turn 1 error: %v", err)
	}

	// Turn 2 — should block first install_package (research gate reset)
	_, _, err = a.ChatTurn("install htop")
	if err != nil {
		t.Fatalf("Turn 2 error: %v", err)
	}

	// Check that a "Research required" event was emitted in turn 2
	foundResearchBlock := false
	for _, e := range *events {
		if e.Type == EventToolResult && strings.Contains(e.Data, "Research required") {
			foundResearchBlock = true
			break
		}
	}
	if !foundResearchBlock {
		t.Error("expected research gate to block in turn 2, but no 'Research required' found")
	}
}

func TestEstimateTokens(t *testing.T) {
	mock := &mockProvider{
		responses: []*providers.Message{
			{Role: "assistant", Content: "Hello!"},
		},
	}
	cfg := defaultTestConfig()
	a := NewWithProvider(mock, cfg, nil, nil)

	// Before any chat, tokens should be 0
	if a.EstimateTokens() != 0 {
		t.Errorf("expected 0 tokens before chat, got %d", a.EstimateTokens())
	}

	a.ChatTurn("test prompt")

	// After chat, should have some tokens
	if a.EstimateTokens() <= 0 {
		t.Error("expected positive token count after chat")
	}
}

func TestMaxRoundsExceeded(t *testing.T) {
	// Create responses that always return tool calls (more than defaultMaxToolRounds)
	responses := make([]*providers.Message, defaultMaxToolRounds+5)
	for i := range responses {
		responses[i] = &providers.Message{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{
					ID:        "call_" + json.Number(string(rune('0'+i%10))).String(),
					Name:      "get_os_info",
					Arguments: "{}",
				},
			},
		}
	}

	mock := &mockProvider{responses: responses}
	cfg := defaultTestConfig()
	cb, _ := collectEvents()

	a := NewWithProvider(mock, cfg, nil, cb)
	_, err := a.Chat("loop forever")
	if err == nil {
		t.Fatal("expected error about max rounds, got nil")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Errorf("expected error about exceeding max rounds, got: %v", err)
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"401", fmt.Errorf("HTTP 401 Unauthorized"), true},
		{"403", fmt.Errorf("HTTP 403 Forbidden"), true},
		{"unauthorized", fmt.Errorf("request unauthorized"), true},
		{"forbidden", fmt.Errorf("access forbidden"), true},
		{"invalid api key", fmt.Errorf("invalid api key provided"), true},
		{"token expired", fmt.Errorf("token expired, please re-authenticate"), true},
		{"credentials expired", fmt.Errorf("credentials expired"), true},
		{"context deadline", fmt.Errorf("context deadline exceeded"), false},
		{"connection refused", fmt.Errorf("connection refused"), false},
		{"timeout", fmt.Errorf("request timeout"), false},
		{"generic error", fmt.Errorf("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAuthError(tt.err)
			if got != tt.want {
				t.Errorf("isAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestCompactHistory(t *testing.T) {
	mock := &mockProvider{
		responses: []*providers.Message{
			{Role: "assistant", Content: "Turn 1 done"},
			{Role: "assistant", Content: "Turn 2 done"},
			{Role: "assistant", Content: "Turn 3 done"},
			{Role: "assistant", Content: "Turn 4 done"},
			{Role: "assistant", Content: "Turn 5 done"},
		},
	}
	cfg := defaultTestConfig()
	a := NewWithProvider(mock, cfg, nil, nil)

	// Run 5 turns to build up messages
	for i := 0; i < 5; i++ {
		a.ChatTurn(fmt.Sprintf("prompt %d", i+1))
	}

	msgCountBefore := len(a.Messages())
	if msgCountBefore < 10 {
		t.Fatalf("expected at least 10 messages before compact, got %d", msgCountBefore)
	}

	// Compact, keeping last 2 turns
	a.CompactHistory(2)

	msgCountAfter := len(a.Messages())
	if msgCountAfter >= msgCountBefore {
		t.Errorf("expected fewer messages after compact: before=%d, after=%d", msgCountBefore, msgCountAfter)
	}

	// System prompt should still be first
	if a.Messages()[0].Role != "system" {
		t.Error("first message should still be system prompt after compact")
	}

	// Should have summary + last 2 turns
	// system + summary_user + summary_assistant + user4 + assistant4 + user5 + assistant5 = 7
	if msgCountAfter > 10 {
		t.Errorf("expected around 7 messages after compacting to 2 turns, got %d", msgCountAfter)
	}
}

func TestContextUsagePercent(t *testing.T) {
	mock := &mockProvider{
		responses: []*providers.Message{
			{Role: "assistant", Content: strings.Repeat("x", 4000)}, // ~1000 tokens
		},
	}
	cfg := defaultTestConfig()
	cfg.Provider.ContextWindow = 2000 // small window for testing
	a := NewWithProvider(mock, cfg, nil, nil)

	// Before chat
	if pct := a.ContextUsagePercent(); pct != 0 {
		t.Errorf("expected 0%% before chat, got %d%%", pct)
	}

	a.ChatTurn("test")

	pct := a.ContextUsagePercent()
	if pct <= 0 {
		t.Errorf("expected positive percentage after chat, got %d%%", pct)
	}
	// With 4000 chars response + system prompt, should be well over 50% of 2000 tokens
	if pct < 30 {
		t.Errorf("expected at least 30%% usage, got %d%%", pct)
	}
}
