package agent

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ZeroSegFault/linx/agent/providers"
	"syscall"
	"time"
)

// Session represents a persistent TUI session stored as a Markdown file.
type Session struct {
	UUID     string
	Started  time.Time
	Model    string
	Profile  string
	Status   string // "active", "archived", "crashed"
	Turns    []Turn
	FilePath string // path to the .md file
	LockPath string // path to the .lock file
}

// Turn represents a single user→assistant exchange within a session.
type Turn struct {
	Number     int
	Timestamp  time.Time
	UserPrompt string
	ToolCalls  []ToolCallRecord
	Response   string
}

// ToolCallRecord is a brief summary of a tool invocation within a turn.
type ToolCallRecord struct {
	Name   string
	Args   string // brief summary, not full JSON
	Result string // brief summary
}

// generateUUID produces a v4 UUID without external dependencies.
func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// SessionsDir returns the base directory for all session data.
func SessionsDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "linx", "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "linx", "sessions")
}

// ActiveDir returns the directory for active (running) sessions.
func ActiveDir() string { return filepath.Join(SessionsDir(), "active") }

// ArchiveDir returns the directory for archived (completed) sessions.
func ArchiveDir() string { return filepath.Join(SessionsDir(), "archive") }

// CrashedDir returns the directory for crashed (unclean shutdown) sessions.
func CrashedDir() string { return filepath.Join(SessionsDir(), "crashed") }

// NewSession creates a new active session, writing the header and lock file to disk.
func NewSession(model, profile string) (*Session, error) {
	id := generateUUID()
	now := time.Now()

	s := &Session{
		UUID:    id,
		Started: now,
		Model:   model,
		Profile: profile,
		Status:  "active",
	}

	if err := os.MkdirAll(ActiveDir(), 0o755); err != nil {
		return nil, err
	}

	s.FilePath = filepath.Join(ActiveDir(), id+".md")
	s.LockPath = filepath.Join(ActiveDir(), id+".lock")

	if err := s.writeHeader(); err != nil {
		return nil, err
	}

	if err := s.writeLock(); err != nil {
		return nil, err
	}

	return s, nil
}

// writeHeader writes (or overwrites) the session Markdown header.
func (s *Session) writeHeader() error {
	header := fmt.Sprintf("# Linx Session\nUUID: %s\nStarted: %s\nModel: %s\nProfile: %s\nStatus: %s\n\n",
		s.UUID,
		s.Started.Format(time.RFC3339),
		s.Model,
		s.Profile,
		s.Status,
	)
	return os.WriteFile(s.FilePath, []byte(header), 0o644)
}

// WriteLock creates/updates the lock file with the current process PID.
func (s *Session) WriteLock() error {
	return s.writeLock()
}

// writeLock writes a lock file containing the current PID.
func (s *Session) writeLock() error {
	content := fmt.Sprintf("PID=%d\nStarted=%s\n", os.Getpid(), s.Started.Format(time.RFC3339))
	return os.WriteFile(s.LockPath, []byte(content), 0o644)
}

// AddTurn appends a turn to the in-memory session and to the Markdown file on disk.
func (s *Session) AddTurn(turn Turn) error {
	s.Turns = append(s.Turns, turn)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Turn %d — %s\n\n", turn.Number, turn.Timestamp.Format("15:04:05")))
	sb.WriteString("### User\n")
	sb.WriteString(turn.UserPrompt + "\n\n")

	if len(turn.ToolCalls) > 0 {
		sb.WriteString("### Tools\n")
		for _, tc := range turn.ToolCalls {
			sb.WriteString(fmt.Sprintf("- %s: %s → %s\n", tc.Name, tc.Args, tc.Result))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("### Assistant\n")
	sb.WriteString(turn.Response + "\n\n")

	f, err := os.OpenFile(s.FilePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(sb.String())
	return err
}

// Archive moves the session from active/ to archive/ and removes the lock file.
func (s *Session) Archive() error {
	if err := os.MkdirAll(ArchiveDir(), 0o755); err != nil {
		return err
	}

	date := s.Started.Format("2006-01-02")
	shortUUID := s.UUID[:8]
	archivePath := filepath.Join(ArchiveDir(), fmt.Sprintf("%s_%s.md", date, shortUUID))

	if err := os.Rename(s.FilePath, archivePath); err != nil {
		return err
	}
	s.FilePath = archivePath
	s.Status = "archived"

	os.Remove(s.LockPath)

	return nil
}

// DetectCrashed scans active/ for lock files whose PIDs are no longer running,
// moves them to crashed/, and returns the detected sessions.
func DetectCrashed() ([]*Session, error) {
	activeDir := ActiveDir()
	entries, err := os.ReadDir(activeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	if err := os.MkdirAll(CrashedDir(), 0o755); err != nil {
		return nil, err
	}

	var crashed []*Session

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}

		lockPath := filepath.Join(activeDir, e.Name())
		pid, err := readLockPID(lockPath)
		if err != nil {
			continue
		}

		if processIsRunning(pid) {
			continue
		}

		// Dead process — crashed session
		uuid := strings.TrimSuffix(e.Name(), ".lock")
		mdPath := filepath.Join(activeDir, uuid+".md")

		sess, loadErr := LoadSession(mdPath)
		if loadErr != nil {
			continue
		}

		crashedMdPath := filepath.Join(CrashedDir(), uuid+".md")
		crashedLockPath := filepath.Join(CrashedDir(), uuid+".lock")
		os.Rename(mdPath, crashedMdPath)
		os.Rename(lockPath, crashedLockPath)

		sess.FilePath = crashedMdPath
		sess.LockPath = crashedLockPath
		sess.Status = "crashed"
		crashed = append(crashed, sess)
	}

	return crashed, nil
}

// readLockPID extracts the PID from a lock file.
func readLockPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PID=") {
			pid, err := strconv.Atoi(strings.TrimPrefix(line, "PID="))
			return pid, err
		}
	}
	return 0, fmt.Errorf("no PID found in lock file")
}

// processIsRunning checks whether a process with the given PID exists.
func processIsRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

// LoadSession parses a session Markdown file and returns a Session.
func LoadSession(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	s := &Session{FilePath: path}
	lines := strings.Split(string(data), "\n")

	// Parse header
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "UUID: ") {
			s.UUID = strings.TrimPrefix(line, "UUID: ")
		} else if strings.HasPrefix(line, "Started: ") {
			t, err := time.Parse(time.RFC3339, strings.TrimPrefix(line, "Started: "))
			if err == nil {
				s.Started = t
			}
		} else if strings.HasPrefix(line, "Model: ") {
			s.Model = strings.TrimPrefix(line, "Model: ")
		} else if strings.HasPrefix(line, "Profile: ") {
			s.Profile = strings.TrimPrefix(line, "Profile: ")
		} else if strings.HasPrefix(line, "Status: ") {
			s.Status = strings.TrimPrefix(line, "Status: ")
		}
	}

	// Parse turns
	turnCount := 0
	var lastPrompt string
	inUser := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Turn ") {
			turnCount++
			inUser = false
		} else if trimmed == "### User" {
			inUser = true
		} else if strings.HasPrefix(trimmed, "### ") {
			inUser = false
		} else if inUser && trimmed != "" {
			lastPrompt = trimmed
		}
	}

	for i := 0; i < turnCount; i++ {
		s.Turns = append(s.Turns, Turn{Number: i + 1})
	}
	if turnCount > 0 && lastPrompt != "" {
		s.Turns[turnCount-1].UserPrompt = lastPrompt
	}

	return s, nil
}

// Summary returns a one-line human-readable summary of the session.
func (s *Session) Summary() string {
	turnCount := len(s.Turns)
	lastPrompt := ""
	if turnCount > 0 {
		lastPrompt = s.Turns[turnCount-1].UserPrompt
		if len(lastPrompt) > 80 {
			lastPrompt = lastPrompt[:80] + "..."
		}
	}
	return fmt.Sprintf("%s  %s  %s/%s  %d turns — %q",
		s.Started.Format("2006-01-02 15:04"),
		s.UUID[:8],
		s.Profile,
		s.Model,
		turnCount,
		lastPrompt,
	)
}

// ListSessions returns all sessions across active, crashed, and archived directories,
// sorted by start time descending (newest first).
func ListSessions() ([]*Session, error) {
	var all []*Session

	for _, dir := range []struct{ path, status string }{
		{ActiveDir(), "active"},
		{CrashedDir(), "crashed"},
		{ArchiveDir(), "archived"},
	} {
		entries, err := os.ReadDir(dir.path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			sess, err := LoadSession(filepath.Join(dir.path, e.Name()))
			if err != nil {
				continue
			}
			sess.Status = dir.status
			all = append(all, sess)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Started.After(all[j].Started)
	})

	return all, nil
}

// FindSession locates a session by UUID prefix or 1-based numeric index.
func FindSession(prefix string) (*Session, error) {
	all, err := ListSessions()
	if err != nil {
		return nil, err
	}

	// Try numeric index first (1-based)
	if num, err := strconv.Atoi(prefix); err == nil && num > 0 && num <= len(all) {
		return all[num-1], nil
	}

	// UUID prefix match
	var matches []*Session
	for _, s := range all {
		if strings.HasPrefix(s.UUID, prefix) {
			matches = append(matches, s)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no session found matching %q", prefix)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous prefix %q — matches %d sessions", prefix, len(matches))
	}
	return matches[0], nil
}

// RestoreFromCrashed moves a crashed session back to active/ with a new lock file.
func RestoreFromCrashed(sess *Session) error {
	if err := os.MkdirAll(ActiveDir(), 0o755); err != nil {
		return err
	}

	uuid := sess.UUID
	activeMd := filepath.Join(ActiveDir(), uuid+".md")
	activeLock := filepath.Join(ActiveDir(), uuid+".lock")

	if err := os.Rename(sess.FilePath, activeMd); err != nil {
		return err
	}
	os.Remove(sess.LockPath)

	sess.FilePath = activeMd
	sess.LockPath = activeLock
	sess.Status = "active"

	return sess.writeLock()
}

// RebuildMessages parses the session file and reconstructs the message history
// for loading into an Agent. Returns system prompt + user/assistant message pairs.
func (s *Session) RebuildMessages(systemPrompt string) []providers.Message {
	msgs := []providers.Message{
		{Role: "system", Content: systemPrompt},
	}

	data, err := os.ReadFile(s.FilePath)
	if err != nil {
		return msgs
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	var currentSection string // "user", "assistant", "tools"
	var sectionContent strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## Turn ") {
			// Flush any pending section
			if currentSection == "assistant" && sectionContent.Len() > 0 {
				msgs = append(msgs, providers.Message{
					Role:    "assistant",
					Content: strings.TrimSpace(sectionContent.String()),
				})
				sectionContent.Reset()
			}
			currentSection = ""
			continue
		}

		if trimmed == "### User" {
			// Flush previous assistant if any
			if currentSection == "assistant" && sectionContent.Len() > 0 {
				msgs = append(msgs, providers.Message{
					Role:    "assistant",
					Content: strings.TrimSpace(sectionContent.String()),
				})
				sectionContent.Reset()
			}
			currentSection = "user"
			sectionContent.Reset()
			continue
		}

		if trimmed == "### Tools" {
			// Flush user content
			if currentSection == "user" && sectionContent.Len() > 0 {
				msgs = append(msgs, providers.Message{
					Role:    "user",
					Content: strings.TrimSpace(sectionContent.String()),
				})
				sectionContent.Reset()
			}
			currentSection = "tools"
			continue
		}

		if trimmed == "### Assistant" {
			// Flush user content if tools section was skipped
			if currentSection == "user" && sectionContent.Len() > 0 {
				msgs = append(msgs, providers.Message{
					Role:    "user",
					Content: strings.TrimSpace(sectionContent.String()),
				})
				sectionContent.Reset()
			}
			currentSection = "assistant"
			sectionContent.Reset()
			continue
		}

		if strings.HasPrefix(trimmed, "### ") || strings.HasPrefix(trimmed, "# ") {
			continue // skip other headers
		}

		// Accumulate content for current section
		if currentSection == "user" || currentSection == "assistant" {
			sectionContent.WriteString(line + "\n")
		}
	}

	// Flush final section
	if currentSection == "assistant" && sectionContent.Len() > 0 {
		msgs = append(msgs, providers.Message{
			Role:    "assistant",
			Content: strings.TrimSpace(sectionContent.String()),
		})
	}

	return msgs
}
