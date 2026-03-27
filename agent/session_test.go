package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// setupTestSessionsDir overrides XDG_DATA_HOME to isolate session tests.
func setupTestSessionsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	return dir
}

func TestNewSession(t *testing.T) {
	setupTestSessionsDir(t)

	sess, err := NewSession("qwen3.5", "local")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Verify fields
	if sess.UUID == "" {
		t.Fatal("UUID should not be empty")
	}
	if sess.Model != "qwen3.5" {
		t.Errorf("Model = %q, want %q", sess.Model, "qwen3.5")
	}
	if sess.Profile != "local" {
		t.Errorf("Profile = %q, want %q", sess.Profile, "local")
	}
	if sess.Status != "active" {
		t.Errorf("Status = %q, want %q", sess.Status, "active")
	}

	// Verify .md file exists in active/
	if _, err := os.Stat(sess.FilePath); err != nil {
		t.Fatalf("session file not found: %v", err)
	}
	if !strings.Contains(sess.FilePath, "active") {
		t.Errorf("FilePath %q should contain 'active'", sess.FilePath)
	}

	// Verify .lock file exists with correct PID
	if _, err := os.Stat(sess.LockPath); err != nil {
		t.Fatalf("lock file not found: %v", err)
	}
	lockData, _ := os.ReadFile(sess.LockPath)
	expectedPID := fmt.Sprintf("PID=%d", os.Getpid())
	if !strings.Contains(string(lockData), expectedPID) {
		t.Errorf("lock file should contain %q, got %q", expectedPID, string(lockData))
	}

	// Verify session file content
	mdData, _ := os.ReadFile(sess.FilePath)
	content := string(mdData)
	if !strings.Contains(content, "UUID: "+sess.UUID) {
		t.Error("session file missing UUID")
	}
	if !strings.Contains(content, "Model: qwen3.5") {
		t.Error("session file missing Model")
	}
}

func TestAddTurn(t *testing.T) {
	setupTestSessionsDir(t)

	sess, err := NewSession("test-model", "test-profile")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	turn := Turn{
		Number:     1,
		Timestamp:  time.Now(),
		UserPrompt: "Install nginx",
		ToolCalls: []ToolCallRecord{
			{Name: "run_command", Args: "apt install nginx", Result: "exit 0"},
		},
		Response: "Installed nginx successfully.",
	}

	if err := sess.AddTurn(turn); err != nil {
		t.Fatalf("AddTurn: %v", err)
	}

	if len(sess.Turns) != 1 {
		t.Fatalf("len(Turns) = %d, want 1", len(sess.Turns))
	}

	// Verify file content
	data, _ := os.ReadFile(sess.FilePath)
	content := string(data)
	if !strings.Contains(content, "## Turn 1") {
		t.Error("file missing Turn 1 header")
	}
	if !strings.Contains(content, "### User") {
		t.Error("file missing User section")
	}
	if !strings.Contains(content, "Install nginx") {
		t.Error("file missing user prompt")
	}
	if !strings.Contains(content, "### Tools") {
		t.Error("file missing Tools section")
	}
	if !strings.Contains(content, "run_command") {
		t.Error("file missing tool call")
	}
	if !strings.Contains(content, "### Assistant") {
		t.Error("file missing Assistant section")
	}
	if !strings.Contains(content, "Installed nginx successfully") {
		t.Error("file missing assistant response")
	}
}

func TestArchive(t *testing.T) {
	setupTestSessionsDir(t)

	sess, err := NewSession("test-model", "test-profile")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	oldMdPath := sess.FilePath
	lockPath := sess.LockPath

	if err := sess.Archive(); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Old file should be gone
	if _, err := os.Stat(oldMdPath); !os.IsNotExist(err) {
		t.Error("old session file should be removed after archive")
	}

	// Lock file should be gone
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file should be removed after archive")
	}

	// New file should exist in archive/
	if _, err := os.Stat(sess.FilePath); err != nil {
		t.Fatalf("archived file not found: %v", err)
	}
	if !strings.Contains(sess.FilePath, "archive") {
		t.Errorf("FilePath %q should contain 'archive'", sess.FilePath)
	}
	if sess.Status != "archived" {
		t.Errorf("Status = %q, want %q", sess.Status, "archived")
	}
}

func TestLoadSession(t *testing.T) {
	setupTestSessionsDir(t)

	sess, err := NewSession("gpt-5", "cloud")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	turn1 := Turn{
		Number:     1,
		Timestamp:  time.Now(),
		UserPrompt: "Configure sshd",
		Response:   "Done configuring sshd.",
	}
	turn2 := Turn{
		Number:     2,
		Timestamp:  time.Now(),
		UserPrompt: "Restart the service",
		Response:   "Service restarted.",
	}
	sess.AddTurn(turn1)
	sess.AddTurn(turn2)

	// Load from disk
	loaded, err := LoadSession(sess.FilePath)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	if loaded.UUID != sess.UUID {
		t.Errorf("UUID = %q, want %q", loaded.UUID, sess.UUID)
	}
	if loaded.Model != "gpt-5" {
		t.Errorf("Model = %q, want %q", loaded.Model, "gpt-5")
	}
	if loaded.Profile != "cloud" {
		t.Errorf("Profile = %q, want %q", loaded.Profile, "cloud")
	}
	if len(loaded.Turns) != 2 {
		t.Fatalf("len(Turns) = %d, want 2", len(loaded.Turns))
	}
	if loaded.Turns[1].UserPrompt != "Restart the service" {
		t.Errorf("last turn prompt = %q, want %q", loaded.Turns[1].UserPrompt, "Restart the service")
	}
}

func TestDetectCrashed(t *testing.T) {
	setupTestSessionsDir(t)

	// Create a session with a dead PID (999999)
	deadUUID := generateUUID()
	if err := os.MkdirAll(ActiveDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	mdPath := filepath.Join(ActiveDir(), deadUUID+".md")
	lockPath := filepath.Join(ActiveDir(), deadUUID+".lock")

	header := fmt.Sprintf("# Linx Session\nUUID: %s\nStarted: %s\nModel: test\nProfile: test\nStatus: active\n\n",
		deadUUID, time.Now().Format(time.RFC3339))
	os.WriteFile(mdPath, []byte(header), 0o644)
	os.WriteFile(lockPath, []byte("PID=999999\nStarted="+time.Now().Format(time.RFC3339)+"\n"), 0o644)

	// Create a session with current PID (should be skipped)
	liveUUID := generateUUID()
	liveMd := filepath.Join(ActiveDir(), liveUUID+".md")
	liveLock := filepath.Join(ActiveDir(), liveUUID+".lock")
	liveHeader := fmt.Sprintf("# Linx Session\nUUID: %s\nStarted: %s\nModel: test\nProfile: test\nStatus: active\n\n",
		liveUUID, time.Now().Format(time.RFC3339))
	os.WriteFile(liveMd, []byte(liveHeader), 0o644)
	os.WriteFile(liveLock, []byte(fmt.Sprintf("PID=%d\nStarted=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))), 0o644)

	crashed, err := DetectCrashed()
	if err != nil {
		t.Fatalf("DetectCrashed: %v", err)
	}

	if len(crashed) != 1 {
		t.Fatalf("expected 1 crashed session, got %d", len(crashed))
	}
	if crashed[0].UUID != deadUUID {
		t.Errorf("crashed UUID = %q, want %q", crashed[0].UUID, deadUUID)
	}
	if crashed[0].Status != "crashed" {
		t.Errorf("crashed Status = %q, want %q", crashed[0].Status, "crashed")
	}

	// Dead session should be moved to crashed/
	if _, err := os.Stat(filepath.Join(CrashedDir(), deadUUID+".md")); err != nil {
		t.Error("crashed session .md not moved to crashed/")
	}

	// Live session should still be in active/
	if _, err := os.Stat(liveMd); err != nil {
		t.Error("live session should still be in active/")
	}
}

func TestSummary(t *testing.T) {
	sess := &Session{
		UUID:    "abcdef12-3456-7890-abcd-ef1234567890",
		Started: time.Date(2026, 3, 27, 14, 30, 0, 0, time.UTC),
		Model:   "qwen3.5",
		Profile: "local",
		Turns: []Turn{
			{Number: 1, UserPrompt: "Install hyprland"},
			{Number: 2, UserPrompt: "Configure waybar"},
		},
	}

	summary := sess.Summary()
	if !strings.Contains(summary, "abcdef12") {
		t.Errorf("summary should contain short UUID, got %q", summary)
	}
	if !strings.Contains(summary, "2026-03-27 14:30") {
		t.Errorf("summary should contain date, got %q", summary)
	}
	if !strings.Contains(summary, "local/qwen3.5") {
		t.Errorf("summary should contain profile/model, got %q", summary)
	}
	if !strings.Contains(summary, "2 turns") {
		t.Errorf("summary should contain turn count, got %q", summary)
	}
	if !strings.Contains(summary, "Configure waybar") {
		t.Errorf("summary should contain last prompt, got %q", summary)
	}
}

func TestListSessions(t *testing.T) {
	setupTestSessionsDir(t)

	// Create one active session
	s1, err := NewSession("model-a", "prof-a")
	if err != nil {
		t.Fatal(err)
	}

	// Create one archived session
	s2, err := NewSession("model-b", "prof-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Archive(); err != nil {
		t.Fatal(err)
	}

	// Create one "crashed" session manually
	crashedUUID := generateUUID()
	os.MkdirAll(CrashedDir(), 0o755)
	crashedHeader := fmt.Sprintf("# Linx Session\nUUID: %s\nStarted: %s\nModel: model-c\nProfile: prof-c\nStatus: crashed\n\n",
		crashedUUID, time.Now().Add(-time.Hour).Format(time.RFC3339))
	os.WriteFile(filepath.Join(CrashedDir(), crashedUUID+".md"), []byte(crashedHeader), 0o644)

	all, err := ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(all) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(all))
	}

	// Check statuses are set
	statuses := map[string]bool{}
	for _, s := range all {
		statuses[s.Status] = true
	}
	if !statuses["active"] || !statuses["archived"] || !statuses["crashed"] {
		t.Errorf("expected all three statuses, got %v", statuses)
	}

	// Verify sorted newest first (s1 and s2 created just now, crashed is 1 hour ago)
	_ = s1
	if all[len(all)-1].UUID != crashedUUID {
		t.Error("oldest session should be last (crashed one from 1 hour ago)")
	}
}

func TestFindSession(t *testing.T) {
	setupTestSessionsDir(t)

	s1, _ := NewSession("model-x", "prof-x")
	// Shift s1's start time back so sort order is deterministic
	s1.Started = s1.Started.Add(-time.Minute)
	s1.writeHeader()

	s2, _ := NewSession("model-y", "prof-y")

	// Find by UUID prefix
	found, err := FindSession(s1.UUID[:8])
	if err != nil {
		t.Fatalf("FindSession by prefix: %v", err)
	}
	if found.UUID != s1.UUID {
		t.Errorf("found UUID = %q, want %q", found.UUID, s1.UUID)
	}

	// Find by numeric index (1-based, sorted newest first)
	// s2 is newer, so s2 is index 1
	found2, err := FindSession("1")
	if err != nil {
		t.Fatalf("FindSession by index: %v", err)
	}
	if found2.UUID != s2.UUID {
		t.Errorf("index 1 should be newest session (s2), got %q", found2.UUID)
	}

	// Find by index 2
	found3, err := FindSession("2")
	if err != nil {
		t.Fatalf("FindSession by index 2: %v", err)
	}
	if found3.UUID != s1.UUID {
		t.Errorf("index 2 should be s1, got %q", found3.UUID)
	}

	// Not found
	_, err = FindSession("zzzznotexist")
	if err == nil {
		t.Error("expected error for non-existent session")
	}

	// Invalid index
	_, err = FindSession(strconv.Itoa(999))
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestRebuildMessages(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	sess, _ := NewSession("test-model", "test-profile")

	sess.AddTurn(Turn{
		Number:     1,
		Timestamp:  time.Now(),
		UserPrompt: "install nginx",
		ToolCalls: []ToolCallRecord{
			{Name: "get_os_info", Args: "{}", Result: "Arch Linux"},
		},
		Response: "Installed nginx successfully.",
	})

	sess.AddTurn(Turn{
		Number:     2,
		Timestamp:  time.Now(),
		UserPrompt: "configure waybar",
		Response:   "Configured waybar with dark theme.",
	})

	msgs := sess.RebuildMessages("You are a test assistant.")

	// Should have: system + user1 + assistant1 + user2 + assistant2 = 5
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(msgs))
	}

	if msgs[0].Role != "system" {
		t.Errorf("first message should be system, got %s", msgs[0].Role)
	}
	if msgs[0].Content != "You are a test assistant." {
		t.Errorf("system prompt mismatch")
	}

	// Check that user messages are present
	foundUser := false
	foundAssistant := false
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, "install nginx") {
			foundUser = true
		}
		if m.Role == "assistant" && strings.Contains(m.Content, "Installed nginx") {
			foundAssistant = true
		}
	}
	if !foundUser {
		t.Error("missing user message for 'install nginx'")
	}
	if !foundAssistant {
		t.Error("missing assistant message for 'Installed nginx'")
	}
}
