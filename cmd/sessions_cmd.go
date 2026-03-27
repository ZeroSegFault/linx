package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"github.com/ZeroSegFault/linx/agent"
	"github.com/ZeroSegFault/linx/config"
	"github.com/ZeroSegFault/linx/tui"
)

func runSessions() error {
	sessions, err := agent.ListSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	// Limit to --last N if set
	limit := flagLast
	if limit <= 0 {
		limit = 10
	}
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}

	fmt.Println("Recent sessions:")
	fmt.Println()
	for i, s := range sessions {
		status := s.Status
		statusIcon := "📁"
		switch status {
		case "active":
			statusIcon = "🟢"
		case "crashed":
			statusIcon = "💥"
		case "archived":
			statusIcon = "📁"
		}

		turnCount := len(s.Turns)
		lastPrompt := ""
		if turnCount > 0 {
			lastPrompt = s.Turns[turnCount-1].UserPrompt
			if len(lastPrompt) > 60 {
				lastPrompt = lastPrompt[:60] + "..."
			}
		}

		fmt.Printf("  %2d. %s [%-8s] %s  %s  %d turns\n",
			i+1, statusIcon, status, s.UUID[:8], s.Started.Format("2006-01-02 15:04"), turnCount)
		if lastPrompt != "" {
			fmt.Printf("      Last: %q\n", lastPrompt)
		}
	}

	fmt.Println("\nUse: lx --resume <number or uuid> to resume a session")
	return nil
}

func runSessionsClear() error {
	sessions, err := agent.ListSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	archived := 0
	for _, s := range sessions {
		if s.Status == "archived" {
			archived++
		}
	}

	if archived == 0 {
		fmt.Println("No archived sessions to clear.")
		return nil
	}

	fmt.Printf("This will delete %d archived session(s).\n", archived)
	fmt.Print("Are you sure? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer != "y" && answer != "yes" {
		fmt.Println("Cancelled.")
		return nil
	}

	deleted := 0
	for _, s := range sessions {
		if s.Status == "archived" {
			os.Remove(s.FilePath)
			deleted++
		}
	}

	fmt.Printf("Deleted %d archived session(s).\n", deleted)
	return nil
}

func runResume(cfg *config.Config, id string) error {
	sess, err := agent.FindSession(id)
	if err != nil {
		return err
	}

	fmt.Printf("Resuming session %s (%s, %d turns)\n", sess.UUID[:8], sess.Started.Format("2006-01-02 15:04"), len(sess.Turns))

	// If it's archived or crashed, restore it to active
	if sess.Status == "crashed" {
		if err := agent.RestoreFromCrashed(sess); err != nil {
			return fmt.Errorf("restoring session: %w", err)
		}
	} else if sess.Status == "archived" {
		// Move back to active
		if err := os.MkdirAll(agent.ActiveDir(), 0o755); err != nil {
			return err
		}
		activePath := filepath.Join(agent.ActiveDir(), sess.UUID+".md")
		if err := os.Rename(sess.FilePath, activePath); err != nil {
			return fmt.Errorf("moving session to active: %w", err)
		}
		sess.FilePath = activePath
		sess.Status = "active"
		sess.LockPath = filepath.Join(agent.ActiveDir(), sess.UUID+".lock")
		sess.WriteLock()
	}

	// Launch TUI with this session
	return tui.RunWithSession(cfg, sess)
}
