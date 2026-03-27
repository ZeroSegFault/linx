package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ZeroSegFault/linx/memory"
)

// BackupEntry tracks a single file backup.
type BackupEntry struct {
	Timestamp    string `json:"timestamp"`
	OriginalPath string `json:"original_path"`
	BackupPath   string `json:"backup_path"`
	SessionID    string `json:"session_id"`
	Description  string `json:"description"`
}

// BackupIndex tracks all backups.
type BackupIndex struct {
	Entries []BackupEntry `json:"entries"`
	mu      sync.Mutex
}

// BackupsDir returns the backups directory path.
func BackupsDir() string {
	return filepath.Join(memory.DataDir(), "backups")
}

// IndexPath returns the path to the backup index.
func IndexPath() string {
	return filepath.Join(BackupsDir(), "index.json")
}

// LoadIndex loads the backup index from disk.
func LoadIndex() (*BackupIndex, error) {
	idx := &BackupIndex{}
	path := IndexPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return idx, nil
		}
		return nil, fmt.Errorf("reading backup index: %w", err)
	}

	if err := json.Unmarshal(data, idx); err != nil {
		return nil, fmt.Errorf("parsing backup index: %w", err)
	}

	return idx, nil
}

// Save writes the index to disk.
func (idx *BackupIndex) Save() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	dir := BackupsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating backups dir: %w", err)
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling backup index: %w", err)
	}

	return os.WriteFile(IndexPath(), data, 0o644)
}

// BackupFile creates a backup of the given file and records it in the index.
// Returns the backup path.
func (idx *BackupIndex) BackupFile(originalPath, sessionID, description string) (string, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	dir := BackupsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating backups dir: %w", err)
	}

	// Read original
	data, err := os.ReadFile(originalPath)
	if err != nil {
		return "", fmt.Errorf("reading original file %s: %w", originalPath, err)
	}

	// Generate backup filename
	baseName := filepath.Base(originalPath)
	timestamp := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(dir, fmt.Sprintf("%s_%s", timestamp, baseName))

	// Avoid collisions
	if _, err := os.Stat(backupPath); err == nil {
		backupPath = filepath.Join(dir, fmt.Sprintf("%s_%d_%s", timestamp, time.Now().UnixNano()%10000, baseName))
	}

	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return "", fmt.Errorf("writing backup: %w", err)
	}

	entry := BackupEntry{
		Timestamp:    time.Now().Format(time.RFC3339),
		OriginalPath: originalPath,
		BackupPath:   backupPath,
		SessionID:    sessionID,
		Description:  description,
	}
	idx.Entries = append(idx.Entries, entry)

	// Prune old entries beyond the limit
	idx.pruneUnlocked(100)

	return backupPath, nil
}

// Prune removes the oldest entries beyond maxEntries, deleting backup files from disk.
func (idx *BackupIndex) Prune(maxEntries int) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.pruneUnlocked(maxEntries)
}

// pruneUnlocked removes old entries without locking (caller must hold the lock).
func (idx *BackupIndex) pruneUnlocked(maxEntries int) {
	if maxEntries <= 0 || len(idx.Entries) <= maxEntries {
		return
	}
	toRemove := idx.Entries[:len(idx.Entries)-maxEntries]
	for _, e := range toRemove {
		_ = os.Remove(e.BackupPath)
	}
	idx.Entries = idx.Entries[len(idx.Entries)-maxEntries:]
}

// Restore restores a backup to its original path.
func Restore(entry BackupEntry) error {
	data, err := os.ReadFile(entry.BackupPath)
	if err != nil {
		return fmt.Errorf("reading backup %s: %w", entry.BackupPath, err)
	}

	dir := filepath.Dir(entry.OriginalPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	if err := os.WriteFile(entry.OriginalPath, data, 0o644); err != nil {
		return fmt.Errorf("restoring to %s: %w", entry.OriginalPath, err)
	}

	return nil
}

// LastN returns the last N backup entries (newest first).
func (idx *BackupIndex) LastN(n int) []BackupEntry {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if n <= 0 || len(idx.Entries) == 0 {
		return nil
	}

	if n > len(idx.Entries) {
		n = len(idx.Entries)
	}

	// Return a reversed copy of the last n entries
	result := make([]BackupEntry, n)
	for i := 0; i < n; i++ {
		result[i] = idx.Entries[len(idx.Entries)-1-i]
	}
	return result
}

// GroupBySession groups entries by session ID.
func (idx *BackupIndex) GroupBySession() map[string][]BackupEntry {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	groups := make(map[string][]BackupEntry)
	for _, e := range idx.Entries {
		sid := e.SessionID
		if sid == "" {
			sid = "(no session)"
		}
		groups[sid] = append(groups[sid], e)
	}
	return groups
}

// FormatReadable formats backup entries for human-readable display.
func FormatEntries(entries []BackupEntry) string {
	if len(entries) == 0 {
		return "No backups found.\n"
	}

	// Group by session
	type sessionGroup struct {
		sessionID string
		entries   []BackupEntry
		latest    time.Time
	}

	groups := make(map[string]*sessionGroup)
	for _, e := range entries {
		sid := e.SessionID
		if sid == "" {
			sid = "(no session)"
		}
		g, ok := groups[sid]
		if !ok {
			g = &sessionGroup{sessionID: sid}
			groups[sid] = g
		}
		g.entries = append(g.entries, e)
		if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
			if t.After(g.latest) {
				g.latest = t
			}
		}
	}

	// Sort sessions by latest timestamp descending
	sorted := make([]*sessionGroup, 0, len(groups))
	for _, g := range groups {
		sorted = append(sorted, g)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].latest.After(sorted[j].latest)
	})

	var sb strings.Builder
	for _, g := range sorted {
		sb.WriteString(fmt.Sprintf("═══ Session: %s ═══\n", g.sessionID))
		for _, e := range g.entries {
			ts := e.Timestamp
			if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
				ts = t.Format("2006-01-02 15:04:05")
			}
			desc := e.Description
			if desc == "" {
				desc = "auto-backup"
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", ts, e.OriginalPath))
			sb.WriteString(fmt.Sprintf("    → %s (%s)\n", filepath.Base(e.BackupPath), desc))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
