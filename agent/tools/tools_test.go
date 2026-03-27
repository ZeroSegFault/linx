package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZeroSegFault/linx/config"
)

func TestGetOSInfo(t *testing.T) {
	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	result, err := r.Execute("get_os_info", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("get_os_info failed: %v", err)
	}

	var info map[string]string
	if err := json.Unmarshal([]byte(result), &info); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Should have at least kernel info
	if info["kernel"] == "" {
		t.Error("expected kernel info, got empty")
	}
	if info["arch"] == "" {
		t.Error("expected arch info, got empty")
	}
}

func TestReadFile(t *testing.T) {
	// Create a temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "hello world\nline two"
	os.WriteFile(path, []byte(content), 0o644)

	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	args, _ := json.Marshal(map[string]string{"path": path})
	result, err := r.Execute("read_file", args)
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}

	if result != content {
		t.Errorf("expected %q, got %q", content, result)
	}
}

func TestReadFileNotFound(t *testing.T) {
	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	args, _ := json.Marshal(map[string]string{"path": "/nonexistent/file.txt"})
	result, err := r.Execute("read_file", args)
	if err != nil {
		t.Fatalf("read_file should not return error for missing file, got: %v", err)
	}
	if !strings.Contains(result, "Error reading file") {
		t.Errorf("expected error message in result, got: %s", result)
	}
}

func TestRunCommand(t *testing.T) {
	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	args, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := r.Execute("run_command", args)
	if err != nil {
		t.Fatalf("run_command failed: %v", err)
	}

	var output map[string]interface{}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if output["output"] != "hello" {
		t.Errorf("expected output 'hello', got %q", output["output"])
	}
	if output["exit_code"].(float64) != 0 {
		t.Errorf("expected exit code 0, got %v", output["exit_code"])
	}
}

func TestRunCommandNonZeroExit(t *testing.T) {
	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	args, _ := json.Marshal(map[string]string{"command": "exit 42"})
	result, err := r.Execute("run_command", args)
	if err != nil {
		t.Fatalf("run_command should not error on non-zero exit: %v", err)
	}

	var output map[string]interface{}
	json.Unmarshal([]byte(result), &output)

	if output["exit_code"].(float64) != 42 {
		t.Errorf("expected exit code 42, got %v", output["exit_code"])
	}
}

func TestWriteFileWithBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.conf")

	// Create original file
	os.WriteFile(path, []byte("original content"), 0o644)

	// Override home for backup path
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

	alwaysConfirm := func(desc string) bool { return true }
	r := NewRegistry(alwaysConfirm, &config.ToolsConfig{}, false, true)

	args, _ := json.Marshal(map[string]interface{}{
		"path":    path,
		"content": "new content",
	})
	result, err := r.Execute("write_file", args)
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}

	if !strings.Contains(result, "Successfully wrote") {
		t.Errorf("expected success message, got: %s", result)
	}

	// Check file was written
	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Errorf("file content wrong: %q", string(data))
	}

	// Check backup exists
	backupDir := filepath.Join(dir, ".local", "share", "linx", "backups")
	entries, _ := os.ReadDir(backupDir)
	if len(entries) == 0 {
		t.Error("expected backup file to be created")
	} else {
		backupPath := filepath.Join(backupDir, entries[0].Name())
		backupData, _ := os.ReadFile(backupPath)
		if string(backupData) != "original content" {
			t.Errorf("backup content wrong: %q", string(backupData))
		}
	}
}

func TestWriteFileDenied(t *testing.T) {
	alwaysDeny := func(desc string) bool { return false }
	r := NewRegistry(alwaysDeny, &config.ToolsConfig{}, false, true)

	args, _ := json.Marshal(map[string]interface{}{
		"path":    "/tmp/should-not-exist.txt",
		"content": "should not be written",
	})
	result, err := r.Execute("write_file", args)
	if err != nil {
		t.Fatalf("write_file should not error on deny: %v", err)
	}

	if !strings.Contains(result, "denied") {
		t.Errorf("expected denial message, got: %s", result)
	}

	if _, err := os.Stat("/tmp/should-not-exist.txt"); err == nil {
		os.Remove("/tmp/should-not-exist.txt")
		t.Error("file should not have been created when user denied")
	}
}

func TestListPackages(t *testing.T) {
	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	result, err := r.Execute("list_packages", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("list_packages failed: %v", err)
	}
	// Should return something (we're on a system with dpkg at minimum)
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestRunCommandPrivilegedRequiresConfirmation(t *testing.T) {
	confirmCalled := false
	denyFn := func(desc string) bool {
		confirmCalled = true
		return false
	}
	r := NewRegistry(denyFn, &config.ToolsConfig{}, false, true)

	args, _ := json.Marshal(map[string]string{"command": "whoami"})
	result, err := r.Execute("run_command_privileged", args)
	if err != nil {
		t.Fatalf("run_command_privileged should not error: %v", err)
	}

	if !confirmCalled {
		t.Error("expected confirmation function to be called")
	}
	if !strings.Contains(result, "denied") {
		t.Errorf("expected denial message, got: %s", result)
	}
}

func TestUnknownTool(t *testing.T) {
	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	_, err := r.Execute("nonexistent_tool", json.RawMessage("{}"))
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestDefinitions(t *testing.T) {
	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	defs := r.Definitions()
	if len(defs) != 14 {
		t.Errorf("expected 14 tool definitions, got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}

	expected := []string{
		"get_os_info", "read_file", "run_command", "run_command_privileged",
		"list_packages", "get_service_status", "read_journal", "write_file",
		"web_search", "fetch_url",
		"get_hardware_info", "manage_service", "install_package", "remove_package",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing tool definition: %s", name)
		}
	}

	// lookup_manpage is only registered when enableManpages=true
	if names["lookup_manpage"] {
		t.Errorf("lookup_manpage should not be registered when enableManpages=false")
	}

	// Verify it IS registered when enabled
	r2 := NewRegistry(nil, &config.ToolsConfig{}, true, true)
	defs2 := r2.Definitions()
	if len(defs2) != 15 {
		t.Errorf("expected 15 tool definitions with manpages enabled, got %d", len(defs2))
	}
}
