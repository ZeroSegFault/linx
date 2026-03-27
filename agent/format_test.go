package agent

import (
	"testing"
)

func TestFormatToolCallSummary(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     string
		contains string
	}{
		{
			name:     "run_command with command",
			toolName: "run_command",
			args:     `{"command":"ls -la"}`,
			contains: "ls -la",
		},
		{
			name:     "read_file with path",
			toolName: "read_file",
			args:     `{"path":"/etc/hosts"}`,
			contains: "/etc/hosts",
		},
		{
			name:     "web_search with query",
			toolName: "web_search",
			args:     `{"query":"arch wifi"}`,
			contains: "arch wifi",
		},
		{
			name:     "install_package with packages",
			toolName: "install_package",
			args:     `{"packages":"nginx"}`,
			contains: "nginx",
		},
		{
			name:     "unknown tool foo",
			toolName: "foo",
			args:     `{"bar":"baz"}`,
			contains: "foo",
		},
		{
			name:     "invalid JSON args",
			toolName: "run_command",
			args:     `not json`,
			contains: "run_command",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatToolCallSummary(tc.toolName, tc.args)
			if !contains(result, tc.contains) {
				t.Errorf("formatToolCallSummary(%q, %q) = %q, want it to contain %q",
					tc.toolName, tc.args, result, tc.contains)
			}
		})
	}
}

func TestFormatToolResult(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		result     string
		contains   []string
		notContain []string
	}{
		{
			name:     "run_command success",
			toolName: "run_command",
			result:   `{"exit_code":0,"output":"hello world"}`,
			contains: []string{"✓"},
		},
		{
			name:     "run_command failure",
			toolName: "run_command",
			result:   `{"exit_code":1,"output":"command not found"}`,
			contains: []string{"✗", "failed"},
		},
		{
			name:     "get_os_info with distro",
			toolName: "get_os_info",
			result:   `{"distro":"Arch Linux","version":"rolling","kernel":"6.8.0","arch":"x86_64"}`,
			contains: []string{"Arch Linux"},
		},
		{
			name:     "web_search text output",
			toolName: "web_search",
			result:   "Some search results\nLine 2\nLine 3",
			contains: []string{"web_search"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatToolResult(tc.toolName, tc.result)
			for _, want := range tc.contains {
				if !contains(result, want) {
					t.Errorf("formatToolResult(%q, ...) = %q, want it to contain %q",
						tc.toolName, result, want)
				}
			}
			for _, bad := range tc.notContain {
				if contains(result, bad) {
					t.Errorf("formatToolResult(%q, ...) = %q, should NOT contain %q",
						tc.toolName, result, bad)
				}
			}
		})
	}
}

func TestIsResearchTool(t *testing.T) {
	researchTools := []string{
		"web_search", "read_file", "lookup_manpage", "get_os_info",
		"get_hardware_info", "get_service_status", "read_journal", "list_packages",
	}
	destructiveTools := []string{
		"install_package", "remove_package", "write_file",
		"run_command_privileged", "manage_service",
	}

	for _, name := range researchTools {
		if !isResearchTool(name) {
			t.Errorf("isResearchTool(%q) = false, want true", name)
		}
	}
	for _, name := range destructiveTools {
		if isResearchTool(name) {
			t.Errorf("isResearchTool(%q) = true, want false", name)
		}
	}
}

func TestIsDestructiveTool(t *testing.T) {
	destructiveTools := []string{
		"install_package", "remove_package", "write_file",
		"run_command_privileged", "manage_service",
	}
	researchTools := []string{
		"web_search", "read_file", "lookup_manpage", "get_os_info",
		"get_hardware_info", "get_service_status", "read_journal", "list_packages",
	}

	for _, name := range destructiveTools {
		if !isDestructiveTool(name) {
			t.Errorf("isDestructiveTool(%q) = false, want true", name)
		}
	}
	for _, name := range researchTools {
		if isDestructiveTool(name) {
			t.Errorf("isDestructiveTool(%q) = true, want false", name)
		}
	}
}

// contains is a helper to check substring presence.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
