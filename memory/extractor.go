package memory

import (
	"fmt"
	"strings"
	"time"
)

// ChatCompleter is an interface for LLM chat completion, used to avoid
// importing the providers package directly (which would create an import cycle).
type ChatCompleter interface {
	ChatCompletion(systemPrompt, userMessage string) (string, error)
}

const extractionPrompt = `You are a memory extraction system for Linx, a Linux systems assistant.

You will be given the current MEMORY.md file and a conversation transcript. Your job is to produce an UPDATED MEMORY.md that incorporates any new durable facts from the conversation.

Rules:
- Keep ALL existing content unless it's been superseded by new information
- Add new facts in the appropriate sections
- Dates should be in YYYY-MM-DD format
- For successful changes, include what was done and which files/commands were involved
- For known issues, include the resolution if it was resolved
- For failed approaches, explain what was tried and why it failed
- Only include things worth remembering across sessions — skip trivial queries
- Maintain the exact Markdown structure shown below

Output ONLY the updated MEMORY.md content. No explanation, no code fencing.

The format MUST be:

# Linx Memory — <actual hostname from system profile>

## System Profile
- **Distro:** <value>
- **Kernel:** <value>
- **Desktop Environment:** <value>
- **Init system:** <value>
- **Package manager:** <value>

## User Preferences
- <preference>

## Successful Changes
- YYYY-MM-DD: <description>

## Known Issues
- YYYY-MM-DD: <problem> — <resolution if any>

## Failed Approaches
- YYYY-MM-DD: <description>`

// ExtractAndUpdate uses the LLM to produce an updated MEMORY.md from a conversation.
// It takes a ChatCompleter, the current memory content, and conversation, returns the updated Markdown.
func ExtractAndUpdate(provider ChatCompleter, currentMemory string, conversation string) (string, error) {
	prompt := extractionPrompt

	userMsg := fmt.Sprintf("Today's date is %s.\n\nCurrent MEMORY.md:\n```\n%s\n```\n\nConversation transcript:\n```\n%s\n```\n\nProduce the updated MEMORY.md:", time.Now().Format("2006-01-02"), currentMemory, conversation)

	response, err := provider.ChatCompletion(prompt, userMsg)
	if err != nil {
		return "", fmt.Errorf("LLM extraction call failed: %w", err)
	}

	// Strip any markdown code fencing the model might add
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```markdown")
	response = strings.TrimPrefix(response, "```md")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Basic validation — must contain the expected heading
	if !strings.Contains(response, "# Linx Memory") {
		return "", fmt.Errorf("LLM response doesn't look like valid MEMORY.md")
	}

	return response + "\n", nil
}
