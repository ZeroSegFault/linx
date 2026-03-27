package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Memory holds the persistent memory structure for Linx.
type Memory struct {
	SystemProfile     SystemProfile
	UserPreferences   []string
	SuccessfulChanges []SuccessfulChange
	KnownIssues       []KnownIssue
	FailedApproaches  []FailedApproach
	mu                sync.RWMutex
}

// SystemProfile captures system information.
type SystemProfile struct {
	Distro         string
	Kernel         string
	DE             string
	InitSystem     string
	PackageManager string
	Hostname       string
}

// SuccessfulChange records a change that was applied successfully.
type SuccessfulChange struct {
	Date        string
	Description string
}

// KnownIssue records a problem and its resolution.
type KnownIssue struct {
	Date       string
	Problem    string
	Resolution string
}

// FailedApproach records an approach that didn't work.
type FailedApproach struct {
	Date        string
	Description string
}

// DataDir returns the Linx data directory.
func DataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "linx")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "linx")
}

// MemoryPath returns the path to MEMORY.md.
func MemoryPath() string {
	return filepath.Join(DataDir(), "MEMORY.md")
}

// HistoryDir returns the path to the history directory.
func HistoryDir() string {
	return filepath.Join(DataDir(), "history")
}

// Load reads memory from MEMORY.md on disk. Returns empty memory if file doesn't exist.
func Load() (*Memory, error) {
	m := &Memory{}
	path := MemoryPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, fmt.Errorf("reading memory file: %w", err)
	}

	parseMarkdown(m, string(data))
	return m, nil
}

// Save writes memory to MEMORY.md on disk.
func (m *Memory) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := MemoryPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating memory dir: %w", err)
	}

	md := m.toMarkdown()
	return os.WriteFile(path, []byte(md), 0o644)
}

// SaveRaw writes raw Markdown content directly to MEMORY.md.
// Used by LLM-based extraction which produces the full Markdown.
func SaveRaw(content string) error {
	path := MemoryPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating memory dir: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// LoadRaw reads the raw MEMORY.md content as a string.
func LoadRaw() (string, error) {
	data, err := os.ReadFile(MemoryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading memory file: %w", err)
	}
	return string(data), nil
}

// Clear wipes all memory.
func (m *Memory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SystemProfile = SystemProfile{}
	m.UserPreferences = nil
	m.SuccessfulChanges = nil
	m.KnownIssues = nil
	m.FailedApproaches = nil
}

// IsEmpty returns true if memory has no meaningful content.
func (m *Memory) IsEmpty() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.isEmptyLocked()
}

// isEmptyLocked is the lock-free version of IsEmpty for use within already-locked methods.
func (m *Memory) isEmptyLocked() bool {
	return m.SystemProfile.Distro == "" &&
		len(m.UserPreferences) == 0 &&
		len(m.SuccessfulChanges) == 0 &&
		len(m.KnownIssues) == 0 &&
		len(m.FailedApproaches) == 0
}

// InjectPrompt returns the raw MEMORY.md content for system prompt injection.
// Truncates to 4000 characters if too long.
func (m *Memory) InjectPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.isEmptyLocked() {
		return ""
	}

	content := m.toMarkdown()
	if len(content) > 4000 {
		content = content[:4000] + "\n\n*[memory truncated]*\n"
	}
	return "\n" + content
}

// AddPreference adds a user preference, deduplicating.
func (m *Memory) AddPreference(pref string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pref = strings.TrimSpace(pref)
	for _, existing := range m.UserPreferences {
		if strings.EqualFold(existing, pref) {
			return
		}
	}
	m.UserPreferences = append(m.UserPreferences, pref)
}

// AddSuccessfulChange adds a successful change record.
func (m *Memory) AddSuccessfulChange(change SuccessfulChange) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if change.Date == "" {
		change.Date = time.Now().Format("2006-01-02")
	}
	m.SuccessfulChanges = append(m.SuccessfulChanges, change)
}

// AddKnownIssue adds or updates a known issue.
func (m *Memory) AddKnownIssue(issue KnownIssue) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if issue.Date == "" {
		issue.Date = time.Now().Format("2006-01-02")
	}

	// Update existing if same problem
	for i, existing := range m.KnownIssues {
		if strings.EqualFold(existing.Problem, issue.Problem) {
			m.KnownIssues[i] = issue
			return
		}
	}
	m.KnownIssues = append(m.KnownIssues, issue)
}

// AddFailedApproach records a failed approach.
func (m *Memory) AddFailedApproach(fa FailedApproach) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if fa.Date == "" {
		fa.Date = time.Now().Format("2006-01-02")
	}
	m.FailedApproaches = append(m.FailedApproaches, fa)
}

// UpdateSystemProfile updates the system profile.
func (m *Memory) UpdateSystemProfile(sp SystemProfile) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SystemProfile = sp
}

// toMarkdown renders the memory as a structured Markdown document.
func (m *Memory) toMarkdown() string {
	var sb strings.Builder

	hostname := m.SystemProfile.Hostname
	if hostname == "" {
		h, err := os.Hostname()
		if err != nil || h == "" {
			hostname = "this system"
		} else {
			hostname = h
		}
	}
	sb.WriteString(fmt.Sprintf("# Linx Memory — %s\n\n", hostname))

	// System Profile
	sb.WriteString("## System Profile\n")
	sp := m.SystemProfile
	if sp.Distro != "" {
		sb.WriteString(fmt.Sprintf("- **Distro:** %s\n", sp.Distro))
	}
	if sp.Kernel != "" {
		sb.WriteString(fmt.Sprintf("- **Kernel:** %s\n", sp.Kernel))
	}
	if sp.DE != "" {
		sb.WriteString(fmt.Sprintf("- **Desktop Environment:** %s\n", sp.DE))
	}
	if sp.InitSystem != "" {
		sb.WriteString(fmt.Sprintf("- **Init system:** %s\n", sp.InitSystem))
	}
	if sp.PackageManager != "" {
		sb.WriteString(fmt.Sprintf("- **Package manager:** %s\n", sp.PackageManager))
	}
	if sp.Distro == "" {
		sb.WriteString("- (not yet captured)\n")
	}
	sb.WriteString("\n")

	// User Preferences
	sb.WriteString("## User Preferences\n")
	if len(m.UserPreferences) == 0 {
		sb.WriteString("- (none recorded)\n")
	} else {
		for _, p := range m.UserPreferences {
			sb.WriteString(fmt.Sprintf("- %s\n", p))
		}
	}
	sb.WriteString("\n")

	// Successful Changes
	sb.WriteString("## Successful Changes\n")
	if len(m.SuccessfulChanges) == 0 {
		sb.WriteString("- (none recorded)\n")
	} else {
		for _, c := range m.SuccessfulChanges {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", c.Date, c.Description))
		}
	}
	sb.WriteString("\n")

	// Known Issues
	sb.WriteString("## Known Issues\n")
	if len(m.KnownIssues) == 0 {
		sb.WriteString("- (none recorded)\n")
	} else {
		for _, i := range m.KnownIssues {
			if i.Resolution != "" {
				sb.WriteString(fmt.Sprintf("- %s: %s — %s\n", i.Date, i.Problem, i.Resolution))
			} else {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", i.Date, i.Problem))
			}
		}
	}
	sb.WriteString("\n")

	// Failed Approaches
	sb.WriteString("## Failed Approaches\n")
	if len(m.FailedApproaches) == 0 {
		sb.WriteString("- (none recorded)\n")
	} else {
		for _, f := range m.FailedApproaches {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", f.Date, f.Description))
		}
	}
	sb.WriteString("")

	return sb.String()
}

// parseMarkdown parses a MEMORY.md into the Memory struct.
func parseMarkdown(m *Memory, content string) {
	lines := strings.Split(content, "\n")
	currentSection := ""

	boldField := regexp.MustCompile(`^\s*-\s+\*\*(.+?):\*\*\s*(.*)$`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect section headings
		if strings.HasPrefix(trimmed, "## ") {
			currentSection = strings.TrimPrefix(trimmed, "## ")
			continue
		}

		// Skip title and empty lines
		if strings.HasPrefix(trimmed, "# ") || trimmed == "" {
			// Extract hostname from title if present
			if strings.HasPrefix(trimmed, "# Linx Memory — ") {
				m.SystemProfile.Hostname = strings.TrimPrefix(trimmed, "# Linx Memory — ")
			}
			continue
		}

		// Skip placeholder entries
		if trimmed == "- (not yet captured)" || trimmed == "- (none recorded)" {
			continue
		}

		switch currentSection {
		case "System Profile":
			if matches := boldField.FindStringSubmatch(trimmed); matches != nil {
				key := strings.TrimSpace(matches[1])
				val := strings.TrimSpace(matches[2])
				switch key {
				case "Distro":
					m.SystemProfile.Distro = val
				case "Kernel":
					m.SystemProfile.Kernel = val
				case "Desktop Environment":
					m.SystemProfile.DE = val
				case "Init system":
					m.SystemProfile.InitSystem = val
				case "Package manager":
					m.SystemProfile.PackageManager = val
				}
			}

		case "User Preferences":
			if strings.HasPrefix(trimmed, "- ") {
				pref := strings.TrimPrefix(trimmed, "- ")
				m.UserPreferences = append(m.UserPreferences, pref)
			}

		case "Successful Changes":
			if strings.HasPrefix(trimmed, "- ") {
				entry := strings.TrimPrefix(trimmed, "- ")
				date, desc := splitDateEntry(entry)
				m.SuccessfulChanges = append(m.SuccessfulChanges, SuccessfulChange{
					Date:        date,
					Description: desc,
				})
			}

		case "Known Issues":
			if strings.HasPrefix(trimmed, "- ") {
				entry := strings.TrimPrefix(trimmed, "- ")
				date, rest := splitDateEntry(entry)
				problem, resolution := splitDash(rest)
				m.KnownIssues = append(m.KnownIssues, KnownIssue{
					Date:       date,
					Problem:    problem,
					Resolution: resolution,
				})
			}

		case "Failed Approaches":
			if strings.HasPrefix(trimmed, "- ") {
				entry := strings.TrimPrefix(trimmed, "- ")
				date, desc := splitDateEntry(entry)
				m.FailedApproaches = append(m.FailedApproaches, FailedApproach{
					Date:        date,
					Description: desc,
				})
			}
		}
	}
}

// splitDateEntry splits "2026-03-27: description" into date and description.
func splitDateEntry(s string) (string, string) {
	// Match YYYY-MM-DD: prefix
	if len(s) >= 11 && s[4] == '-' && s[7] == '-' && s[10] == ':' {
		return s[:10], strings.TrimSpace(s[11:])
	}
	return "", s
}

// splitDash splits "problem — resolution" on em-dash or double-dash.
func splitDash(s string) (string, string) {
	// Try em-dash first
	if idx := strings.Index(s, " — "); idx >= 0 {
		return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+len(" — "):])
	}
	// Try double-dash
	if idx := strings.Index(s, " -- "); idx >= 0 {
		return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+4:])
	}
	return s, ""
}

// AppendHistory writes a session summary to the daily Markdown history log.
func AppendHistory(timestamp time.Time, summary string) error {
	dir := HistoryDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating history dir: %w", err)
	}

	today := timestamp.Format("2006-01-02")
	path := filepath.Join(dir, today+".md")

	timeStr := timestamp.Format("15:04")
	entry := fmt.Sprintf("\n## %s — %s\n\n", timeStr, summary)

	// If file doesn't exist, add a title
	if _, err := os.Stat(path); os.IsNotExist(err) {
		entry = fmt.Sprintf("# Session History — %s\n%s", today, entry)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening history file: %w", err)
	}
	defer f.Close()

	_, err = f.WriteString(entry)
	return err
}
