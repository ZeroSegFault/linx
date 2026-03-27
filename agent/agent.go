package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ZeroSegFault/linx/agent/providers"
	"github.com/ZeroSegFault/linx/agent/tools"
	"github.com/ZeroSegFault/linx/config"
	"github.com/ZeroSegFault/linx/memory"
)

const baseSystemPrompt = `You are Linx, an expert Linux systems assistant. You help users configure, troubleshoot, and manage their Linux systems.

You have access to tools that let you inspect and modify the system. Use them proactively:
- Start by gathering system info with get_os_info when relevant
- Read config files and logs to understand the current state before making changes
- Always use run_command_privileged (not run_command) when sudo is needed
- For file modifications, use write_file which auto-backs up the original

Be concise and practical. Explain what you're doing and why. When a task is done, summarize what was changed.`

const researchPrompt = `
Important: Before running destructive commands (install/remove packages, write files, sudo commands, manage services), ALWAYS research first:
- Use lookup_manpage to check command syntax and flags
- Use get_os_info to understand the system
- Use read_file to check existing config before overwriting
- Use web_search if you're unsure about the best approach`

const researchPromptNoManpages = `
Important: Before running destructive commands (install/remove packages, write files, sudo commands, manage services), ALWAYS research first:
- Use get_os_info to understand the system
- Use read_file to check existing config before overwriting
- Use web_search if you're unsure about the best approach`

const defaultMaxToolRounds = 50

// Callback is called for each step in the agent loop so the UI can display progress.
type Callback func(event Event)

// Event represents something happening in the agent loop.
type Event struct {
	Type    EventType
	Message string // human-readable description
	Data    string // raw data (tool output, LLM response, etc.)
}

// EventType classifies agent events.
type EventType int

const (
	EventThinking    EventType = iota // LLM is being called
	EventToolCall                     // Tool is about to be called
	EventToolResult                   // Tool returned a result
	EventResponse                     // Final text response from LLM
	EventError                        // Something went wrong
	EventStreamChunk                  // Streaming text chunk from LLM
)

// Agent orchestrates LLM interactions with tool calling.
type Agent struct {
	provider      providers.Provider
	tools         *tools.Registry
	callback      Callback
	cfg           *config.Config
	memoryRaw     string              // raw MEMORY.md content for prompt injection
	conversation  strings.Builder     // tracks conversation for memory extraction
	hasResearched bool                // tracks if a research tool was called this session
	messages      []providers.Message // persistent messages for TUI mode
	turnCount     int                 // number of completed turns
}

// New creates a new Agent from the given config.
func New(cfg *config.Config, confirm tools.ConfirmFunc, callback Callback) (*Agent, error) {
	p, err := providers.NewFromConfig(&cfg.Provider)
	if err != nil {
		return nil, fmt.Errorf("creating provider: %w", err)
	}

	// Load raw memory for prompt injection
	memRaw, err := memory.LoadRaw()
	if err != nil {
		memRaw = "" // non-fatal
	}

	// Respect confirm_destructive config — when false, skip all confirmation prompts
	confirmFn := confirm
	if !cfg.Behavior.ConfirmDestructive {
		confirmFn = nil
	}

	return &Agent{
		provider:  p,
		tools:     tools.NewRegistry(confirmFn, &cfg.Tools, cfg.Behavior.EnableManpages, cfg.Behavior.AutoBackup),
		callback:  callback,
		cfg:       cfg,
		memoryRaw: memRaw,
	}, nil
}

// buildSystemPrompt constructs the system prompt with memory context injected.
func (a *Agent) buildSystemPrompt() string {
	prompt := baseSystemPrompt
	if a.cfg.Behavior.EnableManpages {
		prompt += researchPrompt
	} else {
		prompt += researchPromptNoManpages
	}
	if a.memoryRaw != "" {
		memContent := a.memoryRaw
		if len([]rune(memContent)) > 4000 {
			runes := []rune(memContent)
			memContent = string(runes[:4000]) + "\n\n*[memory truncated]*\n"
		}
		prompt += "\n\n" + memContent
	}
	return prompt
}

func (a *Agent) emit(t EventType, msg, data string) {
	if a.callback != nil {
		a.callback(Event{Type: t, Message: msg, Data: data})
	}
}

// agentLoopResult holds the result of a single agent loop run.
type agentLoopResult struct {
	response    string
	toolRecords []ToolCallRecord
}

// runAgentLoop executes the core agent loop: sends messages to the LLM, handles tool calls,
// manages research gate, rate limiting, and auth retry. Returns the final response.
func (a *Agent) runAgentLoop(messages *[]providers.Message, toolDefs []providers.ToolDefinition) (*agentLoopResult, error) {
	var toolRecords []ToolCallRecord

	maxRounds := a.cfg.Behavior.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = defaultMaxToolRounds
	}

	for round := 0; round < maxRounds; round++ {
		a.emit(EventThinking, "Thinking...", "")

		onChunk := func(chunk string) {
			a.emit(EventStreamChunk, chunk, chunk)
		}

		resp, err := a.provider.ChatWithToolsStream(*messages, toolDefs, onChunk)
		if err != nil {
			// Check if this is an auth error — try refreshing the provider
			if isAuthError(err) {
				newProvider, refreshErr := providers.NewFromConfig(&a.cfg.Provider)
				if refreshErr == nil {
					a.provider = newProvider
					resp, err = a.provider.ChatWithToolsStream(*messages, toolDefs, onChunk)
				}
			}
			if err != nil {
				a.emit(EventError, fmt.Sprintf("LLM error: %v", err), err.Error())
				return nil, fmt.Errorf("LLM error: %w", err)
			}
		}

		*messages = append(*messages, *resp)

		// If no tool calls, we have our final answer
		if len(resp.ToolCalls) == 0 {
			a.emit(EventResponse, resp.Content, resp.Content)
			a.conversation.WriteString(fmt.Sprintf("Assistant: %s\n", resp.Content))
			return &agentLoopResult{
				response:    resp.Content,
				toolRecords: toolRecords,
			}, nil
		}

		// Rate-limit tool calls per round
		maxTools := a.cfg.Behavior.MaxToolsPerRound
		if maxTools <= 0 {
			maxTools = 10
		}
		toolCalls := resp.ToolCalls
		var skippedCalls []providers.ToolCall
		if len(toolCalls) > maxTools {
			skippedCalls = toolCalls[maxTools:]
			toolCalls = toolCalls[:maxTools]
		}

		// Process each tool call
		for _, tc := range toolCalls {
			a.emit(EventToolCall, formatToolCallSummary(tc.Name, tc.Arguments), tc.Arguments)
			a.conversation.WriteString(fmt.Sprintf("Tool call: %s(%s)\n", tc.Name, tc.Arguments))

			var result string
			var execErr error

			if a.cfg.Behavior.RequireResearch && isDestructiveTool(tc.Name) && !a.hasResearched {
				result = "⚠️ Research required: Before performing destructive operations, you must first gather information about the system and task. Use tools like get_os_info, lookup_manpage, web_search, read_file, or list_packages to understand the current state before making changes."
			} else {
				result, execErr = a.tools.Execute(tc.Name, json.RawMessage(tc.Arguments))
				if execErr != nil {
					result = fmt.Sprintf("Tool error: %v", execErr)
				}
				if isResearchTool(tc.Name) {
					a.hasResearched = true
				}
			}

			toolRecords = append(toolRecords, ToolCallRecord{
				Name:   tc.Name,
				Args:   truncateForMemory(tc.Arguments, 100),
				Result: truncateForMemory(result, 100),
			})

			a.emit(EventToolResult, formatToolResult(tc.Name, result), result)
			a.conversation.WriteString(fmt.Sprintf("Tool result: %s\n", truncateForMemory(result, 200)))

			*messages = append(*messages, providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}

		// Add skip messages for rate-limited tool calls
		for _, tc := range skippedCalls {
			skipMsg := fmt.Sprintf("Tool call skipped — too many tool calls in one response. Maximum is %d per round.", maxTools)
			a.emit(EventToolResult, fmt.Sprintf("⚠️ %s: %s", tc.Name, skipMsg), skipMsg)
			*messages = append(*messages, providers.Message{
				Role:       "tool",
				Content:    skipMsg,
				ToolCallID: tc.ID,
			})
		}
	}

	return nil, fmt.Errorf("agent loop exceeded %d rounds without a final answer", maxRounds)
}

// Chat sends a prompt and runs the full agent loop (tool calls + responses).
// Returns the final text response.
func (a *Agent) Chat(prompt string) (string, error) {
	systemPrompt := a.buildSystemPrompt()
	a.conversation.WriteString(fmt.Sprintf("User: %s\n", prompt))

	messages := []providers.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	toolDefs := a.tools.Definitions()

	result, err := a.runAgentLoop(&messages, toolDefs)
	if err != nil {
		return "", err
	}

	return result.response, nil
}

// ExtractAndSaveMemory extracts durable facts from the conversation and saves to MEMORY.md.
func (a *Agent) ExtractAndSaveMemory() {
	a.ExtractAndSaveMemoryFromSnapshot(a.conversation.String())
}

// ConversationSnapshot returns a copy of the current conversation for async extraction.
func (a *Agent) ConversationSnapshot() string {
	return a.conversation.String()
}

// ExtractAndSaveMemoryFromSnapshot extracts memory from a conversation snapshot.
func (a *Agent) ExtractAndSaveMemoryFromSnapshot(conv string) {
	if conv == "" {
		return
	}

	updatedMd, err := memory.ExtractAndUpdate(a.provider, a.memoryRaw, conv)
	if err != nil {
		return
	}

	_ = memory.SaveRaw(updatedMd)

	summary := "Session completed"
	if idx := strings.Index(conv, "\n"); idx > 0 {
		line := conv[:idx]
		line = strings.TrimPrefix(line, "User: ")
		if len(line) > 80 {
			line = line[:80] + "..."
		}
		summary = line
	}
	_ = memory.AppendHistory(time.Now(), summary)
}

// BuildSystemPrompt returns the constructed system prompt (exported for session restore).
func (a *Agent) BuildSystemPrompt() string {
	return a.buildSystemPrompt()
}

// ChatTurn sends a prompt within a persistent session.
// Unlike Chat(), it appends to existing messages instead of starting fresh.
// After the response, hasResearched is reset (Option A: per-turn reset).
// Returns the response text and a Turn record for session logging.
func (a *Agent) ChatTurn(prompt string) (string, Turn, error) {
	if len(a.messages) == 0 {
		systemPrompt := a.buildSystemPrompt()
		a.messages = []providers.Message{
			{Role: "system", Content: systemPrompt},
		}
	}

	a.messages = append(a.messages, providers.Message{
		Role: "user", Content: prompt,
	})

	a.conversation.WriteString(fmt.Sprintf("User: %s\n", prompt))

	toolDefs := a.tools.Definitions()

	result, err := a.runAgentLoop(&a.messages, toolDefs)
	if err != nil {
		return "", Turn{}, err
	}

	// Reset research gate per-turn
	a.hasResearched = false

	// Check compaction
	compactThreshold := a.cfg.Behavior.CompactThreshold
	if compactThreshold <= 0 {
		compactThreshold = 90
	}
	if a.ContextUsagePercent() >= compactThreshold {
		a.CompactHistory(3)
		a.emit(EventThinking, "Context compacted to free space", "")
	}

	turn := Turn{
		Number:     a.turnCount + 1,
		Timestamp:  time.Now(),
		UserPrompt: prompt,
		ToolCalls:  result.toolRecords,
		Response:   result.response,
	}
	a.turnCount++

	return result.response, turn, nil
}

// EstimateTokens returns a rough token count based on total message content.
// Uses chars/4 as approximation.
func (a *Agent) EstimateTokens() int {
	total := 0
	for _, m := range a.messages {
		total += len(m.Content)
		for _, tc := range m.ToolCalls {
			total += len(tc.Arguments)
		}
	}
	return total / 4
}

// ContextUsagePercent returns the estimated context usage as a percentage.
func (a *Agent) ContextUsagePercent() int {
	contextWindow := a.cfg.Provider.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 32000
	}
	tokens := a.EstimateTokens()
	pct := (tokens * 100) / contextWindow
	if pct > 100 {
		pct = 100
	}
	return pct
}

// TurnCount returns the number of completed turns.
func (a *Agent) TurnCount() int {
	return a.turnCount
}

// Messages returns the current message history (for session saving/restoring).
func (a *Agent) Messages() []providers.Message {
	return a.messages
}

// LoadMessages restores messages from a previous session.
func (a *Agent) LoadMessages(msgs []providers.Message) {
	a.messages = msgs
}

// CompactHistory summarises old messages to free context space.
// Keeps the system prompt and the last keepTurns worth of user/assistant/tool messages.
func (a *Agent) CompactHistory(keepTurns int) error {
	if keepTurns <= 0 {
		keepTurns = 3
	}

	if len(a.messages) <= 1 {
		return nil // nothing to compact (only system prompt or empty)
	}

	// Find turn boundaries (user messages mark new turns)
	var turnStarts []int
	for i, m := range a.messages {
		if m.Role == "user" {
			turnStarts = append(turnStarts, i)
		}
	}

	if len(turnStarts) <= keepTurns {
		return nil // not enough turns to compact
	}

	// Split: keep system prompt + compact old turns + keep recent turns
	cutoff := turnStarts[len(turnStarts)-keepTurns]

	systemPrompt := a.messages[0] // always the system prompt
	oldMessages := a.messages[1:cutoff]
	recentMessages := a.messages[cutoff:]

	// Build summary of old messages
	var summaryBuilder strings.Builder
	summaryBuilder.WriteString("Summary of previous conversation:\n")
	for _, m := range oldMessages {
		switch m.Role {
		case "user":
			summaryBuilder.WriteString(fmt.Sprintf("User asked: %s\n", truncateForMemory(m.Content, 200)))
		case "assistant":
			if m.Content != "" {
				summaryBuilder.WriteString(fmt.Sprintf("Assistant: %s\n", truncateForMemory(m.Content, 200)))
			}
		case "tool":
			summaryBuilder.WriteString(fmt.Sprintf("Tool result: %s\n", truncateForMemory(m.Content, 100)))
		}
	}

	// Create compacted message list
	summaryMsg := providers.Message{
		Role:    "user",
		Content: summaryBuilder.String(),
	}
	summaryResponse := providers.Message{
		Role:    "assistant",
		Content: "Understood. I have the context from our previous conversation and will continue from here.",
	}

	a.messages = make([]providers.Message, 0, 2+len(recentMessages)+2)
	a.messages = append(a.messages, systemPrompt)
	a.messages = append(a.messages, summaryMsg)
	a.messages = append(a.messages, summaryResponse)
	a.messages = append(a.messages, recentMessages...)

	return nil
}

// truncateForMemory limits a string to maxLen characters for conversation tracking.
func truncateForMemory(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// isAuthError checks if an error is likely an authentication/authorization failure.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "invalid api key") ||
		strings.Contains(msg, "token expired") ||
		strings.Contains(msg, "credentials expired")
}
