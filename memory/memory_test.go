package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryLoadSaveRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Load from non-existent file should return empty memory
	mem, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !mem.IsEmpty() {
		t.Fatal("expected empty memory")
	}

	// Add data and save
	mem.UpdateSystemProfile(SystemProfile{
		Distro:         "Arch Linux (rolling)",
		Kernel:         "6.8.0",
		DE:             "KDE Plasma (Wayland)",
		InitSystem:     "systemd",
		PackageManager: "pacman",
		Hostname:       "testbox",
	})
	mem.AddPreference("Prefers vim over nano")
	mem.AddPreference("Uses Wayland not X11")
	mem.AddSuccessfulChange(SuccessfulChange{
		Date:        "2026-03-27",
		Description: "Configured NetworkManager to auto-connect to HomeWifi",
	})
	mem.AddKnownIssue(KnownIssue{
		Date:       "2026-03-26",
		Problem:    "Bluetooth not working after suspend",
		Resolution: "resolved by reloading btusb module",
	})
	mem.AddFailedApproach(FailedApproach{
		Date:        "2026-03-26",
		Description: "Tried installing nvidia-dkms for GPU — failed due to kernel version mismatch, use nvidia instead",
	})

	if err := mem.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file is Markdown
	data, err := os.ReadFile(filepath.Join(tmpDir, "linx", "MEMORY.md"))
	if err != nil {
		t.Fatalf("reading MEMORY.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Linx Memory — testbox") {
		t.Error("expected Markdown title with hostname")
	}
	if !strings.Contains(content, "## System Profile") {
		t.Error("expected System Profile section")
	}
	if !strings.Contains(content, "**Distro:** Arch Linux (rolling)") {
		t.Error("expected distro in System Profile")
	}

	// Reload and verify roundtrip
	mem2, err := Load()
	if err != nil {
		t.Fatalf("Load() after save error: %v", err)
	}

	if mem2.SystemProfile.Distro != "Arch Linux (rolling)" {
		t.Errorf("expected distro 'Arch Linux (rolling)', got %q", mem2.SystemProfile.Distro)
	}
	if mem2.SystemProfile.Hostname != "testbox" {
		t.Errorf("expected hostname 'testbox', got %q", mem2.SystemProfile.Hostname)
	}
	if len(mem2.UserPreferences) != 2 {
		t.Errorf("expected 2 preferences, got %d", len(mem2.UserPreferences))
	}
	if len(mem2.SuccessfulChanges) != 1 {
		t.Errorf("expected 1 successful change, got %d", len(mem2.SuccessfulChanges))
	}
	if mem2.SuccessfulChanges[0].Date != "2026-03-27" {
		t.Errorf("expected date '2026-03-27', got %q", mem2.SuccessfulChanges[0].Date)
	}
	if len(mem2.KnownIssues) != 1 {
		t.Errorf("expected 1 known issue, got %d", len(mem2.KnownIssues))
	}
	if mem2.KnownIssues[0].Resolution == "" {
		t.Error("expected known issue to have resolution")
	}
	if len(mem2.FailedApproaches) != 1 {
		t.Errorf("expected 1 failed approach, got %d", len(mem2.FailedApproaches))
	}
}

func TestPreferenceDedup(t *testing.T) {
	mem := &Memory{}
	mem.AddPreference("I use Wayland")
	mem.AddPreference("I USE WAYLAND") // should dedup (case-insensitive)
	mem.AddPreference("I prefer vim")

	if len(mem.UserPreferences) != 2 {
		t.Errorf("expected 2 preferences after dedup, got %d", len(mem.UserPreferences))
	}
}

func TestKnownIssueUpdate(t *testing.T) {
	mem := &Memory{}
	mem.AddKnownIssue(KnownIssue{
		Problem: "Bluetooth not working",
	})
	mem.AddKnownIssue(KnownIssue{
		Problem:    "Bluetooth not working",
		Resolution: "Installed bluez-utils",
	})

	if len(mem.KnownIssues) != 1 {
		t.Errorf("expected 1 known issue after update, got %d", len(mem.KnownIssues))
	}
	if mem.KnownIssues[0].Resolution != "Installed bluez-utils" {
		t.Errorf("expected updated resolution, got %q", mem.KnownIssues[0].Resolution)
	}
}

func TestInjectPromptEmpty(t *testing.T) {
	mem := &Memory{}
	if prompt := mem.InjectPrompt(); prompt != "" {
		t.Errorf("expected empty prompt for empty memory, got %q", prompt)
	}
}

func TestInjectPromptWithData(t *testing.T) {
	mem := &Memory{}
	mem.UpdateSystemProfile(SystemProfile{
		Distro:         "Arch Linux",
		Kernel:         "6.8.0",
		DE:             "KDE",
		InitSystem:     "systemd",
		PackageManager: "pacman",
	})
	mem.AddPreference("I prefer vim")

	prompt := mem.InjectPrompt()
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "Arch Linux") {
		t.Error("expected prompt to contain distro name")
	}
	if !strings.Contains(prompt, "vim") {
		t.Error("expected prompt to contain user preference")
	}
	if !strings.Contains(prompt, "# Linx Memory") {
		t.Error("expected prompt to contain Markdown heading")
	}
}

func TestInjectPromptTruncation(t *testing.T) {
	mem := &Memory{}
	mem.UpdateSystemProfile(SystemProfile{Distro: "TestOS"})
	// Add many preferences to exceed 4000 chars
	for i := 0; i < 200; i++ {
		mem.UserPreferences = append(mem.UserPreferences, strings.Repeat("x", 50))
	}

	prompt := mem.InjectPrompt()
	if len(prompt) > 4100 { // some slack for the truncation message and leading newline
		t.Errorf("expected prompt to be truncated, got %d chars", len(prompt))
	}
	if !strings.Contains(prompt, "*[memory truncated]*") {
		t.Error("expected truncation marker")
	}
}

func TestAppendHistory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	now, _ := time.Parse(time.RFC3339, "2026-03-27T14:30:00+11:00")
	err := AppendHistory(now, "Configured WiFi successfully")
	if err != nil {
		t.Fatalf("AppendHistory error: %v", err)
	}

	// Verify file was created
	path := filepath.Join(tmpDir, "linx", "history", "2026-03-27.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading history file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# Session History — 2026-03-27") {
		t.Error("expected history title")
	}
	if !strings.Contains(content, "Configured WiFi successfully") {
		t.Error("expected session entry")
	}

	// Append another entry
	now2, _ := time.Parse(time.RFC3339, "2026-03-27T16:45:00+11:00")
	err = AppendHistory(now2, "Fixed bluetooth issue")
	if err != nil {
		t.Fatalf("second AppendHistory error: %v", err)
	}

	data, _ = os.ReadFile(path)
	content = string(data)
	if !strings.Contains(content, "Fixed bluetooth issue") {
		t.Error("expected second session entry")
	}
}

func TestLoadRawSaveRaw(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	rawContent := "# Linx Memory — testbox\n\n## System Profile\n- **Distro:** Ubuntu\n"
	err := SaveRaw(rawContent)
	if err != nil {
		t.Fatalf("SaveRaw error: %v", err)
	}

	loaded, err := LoadRaw()
	if err != nil {
		t.Fatalf("LoadRaw error: %v", err)
	}
	if loaded != rawContent {
		t.Errorf("LoadRaw mismatch:\ngot:  %q\nwant: %q", loaded, rawContent)
	}
}

func TestParseMarkdownEdgeCases(t *testing.T) {
	md := `# Linx Memory — myhost

## System Profile
- **Distro:** Ubuntu 24.04
- **Kernel:** 6.8.0

## User Preferences
- Prefers nano

## Successful Changes
- 2026-03-27: Installed htop

## Known Issues
- 2026-03-26: Screen tearing — enabled compositing

## Failed Approaches
- 2026-03-26: Tried Xorg — crashes on startup
`

	mem := &Memory{}
	parseMarkdown(mem, md)

	if mem.SystemProfile.Distro != "Ubuntu 24.04" {
		t.Errorf("distro = %q, want 'Ubuntu 24.04'", mem.SystemProfile.Distro)
	}
	if mem.SystemProfile.Hostname != "myhost" {
		t.Errorf("hostname = %q, want 'myhost'", mem.SystemProfile.Hostname)
	}
	if len(mem.UserPreferences) != 1 || mem.UserPreferences[0] != "Prefers nano" {
		t.Errorf("preferences = %v, want ['Prefers nano']", mem.UserPreferences)
	}
	if len(mem.SuccessfulChanges) != 1 || mem.SuccessfulChanges[0].Description != "Installed htop" {
		t.Errorf("changes = %v", mem.SuccessfulChanges)
	}
	if len(mem.KnownIssues) != 1 || mem.KnownIssues[0].Resolution != "enabled compositing" {
		t.Errorf("issues = %v", mem.KnownIssues)
	}
	if len(mem.FailedApproaches) != 1 {
		t.Errorf("failed = %v", mem.FailedApproaches)
	}
}
