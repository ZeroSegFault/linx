package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZeroSegFault/linx/memory"
)

func TestBackupAndRestore(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Create a file to back up
	testFile := filepath.Join(tmpDir, "test.conf")
	originalContent := "original content\n"
	if err := os.WriteFile(testFile, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	// Load index (should be empty)
	idx, err := LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if len(idx.Entries) != 0 {
		t.Fatalf("expected empty index, got %d entries", len(idx.Entries))
	}

	// Backup the file
	backupPath, err := idx.BackupFile(testFile, "session-1", "test backup")
	if err != nil {
		t.Fatalf("BackupFile: %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected non-empty backup path")
	}

	// Save index
	if err := idx.Save(); err != nil {
		t.Fatalf("Save index: %v", err)
	}

	// Verify backup exists
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(backupData) != originalContent {
		t.Errorf("backup content mismatch: got %q", string(backupData))
	}

	// Modify original
	if err := os.WriteFile(testFile, []byte("modified content\n"), 0o644); err != nil {
		t.Fatalf("modifying test file: %v", err)
	}

	// Restore from backup
	if err := Restore(idx.Entries[0]); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify restoration
	restored, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("reading restored file: %v", err)
	}
	if string(restored) != originalContent {
		t.Errorf("restored content mismatch: got %q, want %q", string(restored), originalContent)
	}
}

func TestLastN(t *testing.T) {
	// Ensure memory.DataDir() points to temp
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	_ = memory.DataDir() // force re-eval

	idx := &BackupIndex{}

	// Add some entries
	for i := 0; i < 5; i++ {
		testFile := filepath.Join(tmpDir, "file.txt")
		os.WriteFile(testFile, []byte("data"), 0o644)
		_, _ = idx.BackupFile(testFile, "sess", "backup")
	}

	last3 := idx.LastN(3)
	if len(last3) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(last3))
	}

	// LastN with n > count
	all := idx.LastN(100)
	if len(all) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(all))
	}
}

func TestFormatEntries(t *testing.T) {
	// Empty
	output := FormatEntries(nil)
	if output != "No backups found.\n" {
		t.Errorf("unexpected output for empty: %q", output)
	}

	// With entries
	entries := []BackupEntry{
		{
			Timestamp:    "2026-03-26T10:00:00+11:00",
			OriginalPath: "/etc/test.conf",
			BackupPath:   "/tmp/backup/test.conf",
			SessionID:    "abc",
			Description:  "before edit",
		},
	}
	output = FormatEntries(entries)
	if output == "" || output == "No backups found.\n" {
		t.Error("expected non-empty formatted output")
	}
}

func TestReloadIndex(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	testFile := filepath.Join(tmpDir, "data.txt")
	os.WriteFile(testFile, []byte("hello"), 0o644)

	idx, _ := LoadIndex()
	idx.BackupFile(testFile, "s1", "test")
	idx.Save()

	// Reload
	idx2, err := LoadIndex()
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if len(idx2.Entries) != 1 {
		t.Fatalf("expected 1 entry after reload, got %d", len(idx2.Entries))
	}
}
