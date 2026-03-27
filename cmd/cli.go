package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ZeroSegFault/linx/agent"
	"github.com/ZeroSegFault/linx/config"
	"golang.org/x/sys/unix"
)

// cliConfirm asks the user for y/n confirmation on stdout/stdin.
func cliConfirm(description string) bool {
	fmt.Printf("\n⚠️  %s\n", description)
	fmt.Print("Proceed? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

// cliCallback prints agent events to stdout for CLI mode.
func cliCallback(event agent.Event) {
	switch event.Type {
	case agent.EventThinking:
		fmt.Printf("💭 %s\n", event.Message)
	case agent.EventToolCall:
		fmt.Printf("%s\n", event.Message)
	case agent.EventToolResult:
		fmt.Printf("%s\n", event.Message)
	case agent.EventStreamChunk:
		fmt.Print(event.Data)
	case agent.EventError:
		fmt.Fprintf(os.Stderr, "❌ %s\n", event.Message)
	}
}

func runCLI(cfg *config.Config, prompt string) error {
	a, err := agent.New(cfg, cliConfirm, cliCallback)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	_, err = a.Chat(prompt)
	if err != nil {
		return fmt.Errorf("agent error: %w", err)
	}

	// Print newline after streamed content
	fmt.Println()

	// Extract and save memory after session
	a.ExtractAndSaveMemory()

	return nil
}

// isTerminal returns true if stdin is a terminal.
func isTerminal() bool {
	_, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
	return err == nil
}
