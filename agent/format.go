package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// formatToolCallSummary returns a human-readable summary of a tool call.
func formatToolCallSummary(name string, args string) string {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return fmt.Sprintf("⚙️  %s", name)
	}

	getStr := func(key string) string {
		if v, ok := parsed[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	switch name {
	case "run_command", "run_command_privileged":
		if cmd := getStr("command"); cmd != "" {
			return fmt.Sprintf("⚙️  %s: %s", name, cmd)
		}
	case "read_file":
		if path := getStr("path"); path != "" {
			return fmt.Sprintf("⚙️  read_file: %s", path)
		}
	case "write_file":
		if path := getStr("path"); path != "" {
			return fmt.Sprintf("⚙️  write_file: %s", path)
		}
	case "web_search":
		if query := getStr("query"); query != "" {
			return fmt.Sprintf("⚙️  web_search: %q", query)
		}
	case "list_packages":
		if filter := getStr("filter"); filter != "" {
			return fmt.Sprintf("⚙️  list_packages: %s", filter)
		}
		return "⚙️  list_packages"
	case "get_service_status":
		if unit := getStr("unit"); unit != "" {
			return fmt.Sprintf("⚙️  get_service_status: %s", unit)
		}
	case "manage_service":
		if unit := getStr("unit"); unit != "" {
			action := getStr("action")
			if action != "" {
				return fmt.Sprintf("⚙️  manage_service: %s %s", action, unit)
			}
			return fmt.Sprintf("⚙️  manage_service: %s", unit)
		}
	case "install_package":
		if pkg := getStr("packages"); pkg != "" {
			return fmt.Sprintf("⚙️  install_package: %s", pkg)
		}
	case "remove_package":
		if pkg := getStr("packages"); pkg != "" {
			return fmt.Sprintf("⚙️  remove_package: %s", pkg)
		}
	}

	return fmt.Sprintf("⚙️  %s", name)
}

// isResearchTool returns true if the tool is considered a research/information-gathering tool.
func isResearchTool(name string) bool {
	switch name {
	case "web_search", "read_file", "lookup_manpage", "get_os_info",
		"get_hardware_info", "get_service_status", "read_journal", "list_packages":
		return true
	}
	return false
}

// isDestructiveTool returns true if the tool modifies the system.
func isDestructiveTool(name string) bool {
	switch name {
	case "install_package", "remove_package", "write_file",
		"run_command_privileged", "manage_service":
		return true
	}
	return false
}

// formatToolResult returns a human-readable summary of a tool result.
func formatToolResult(name string, result string) string {
	const maxDisplay = 500

	prefixLines := func(lines []string, prefix string) string {
		var b strings.Builder
		for _, l := range lines {
			b.WriteString(prefix)
			b.WriteString(l)
			b.WriteString("\n")
		}
		return b.String()
	}

	firstNLines := func(s string, n int) ([]string, int) {
		all := strings.Split(s, "\n")
		// Trim trailing empty lines
		for len(all) > 0 && strings.TrimSpace(all[len(all)-1]) == "" {
			all = all[:len(all)-1]
		}
		total := len(all)
		if total > n {
			return all[:n], total
		}
		return all, total
	}

	capOutput := func(s string) string {
		runes := []rune(s)
		if len(runes) > maxDisplay {
			return string(runes[:maxDisplay]) + "…"
		}
		return s
	}

	switch name {
	case "run_command", "run_command_privileged":
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(result), &parsed); err == nil {
			header := fmt.Sprintf("✓ %s done", name)

			// Check exit code
			exitCode := 0
			if ec, ok := parsed["exit_code"]; ok {
				if f, ok := ec.(float64); ok {
					exitCode = int(f)
				}
			}
			if exitCode != 0 {
				header = fmt.Sprintf("✗ %s failed (exit %d)", name, exitCode)
			}

			if output, ok := parsed["output"]; ok {
				if s, ok := output.(string); ok && strings.TrimSpace(s) != "" {
					lines, total := firstNLines(s, 8)
					body := prefixLines(lines, "│ ")
					if total > 8 {
						body += fmt.Sprintf("│ ... (%d more lines)\n", total-8)
					}
					return capOutput(header + "\n" + body)
				}
			}
			return header
		}

	case "read_file":
		if strings.TrimSpace(result) != "" {
			lines, total := firstNLines(result, 5)
			body := prefixLines(lines, "│ ")
			header := fmt.Sprintf("✓ read_file done (%d lines)", total)
			if total > 5 {
				body += fmt.Sprintf("│ ... (%d more lines)\n", total-5)
			}
			return capOutput(header + "\n" + body)
		}

	case "get_os_info":
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(result), &parsed); err == nil {
			var parts []string
			for _, key := range []string{"distro", "version", "kernel", "arch"} {
				if v, ok := parsed[key]; ok {
					if s, ok := v.(string); ok && s != "" {
						parts = append(parts, s)
					}
				}
			}
			if len(parts) > 0 {
				return fmt.Sprintf("✓ get_os_info: %s", strings.Join(parts, " · "))
			}
		}

	case "get_hardware_info":
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(result), &parsed); err == nil {
			var parts []string
			for _, key := range []string{"cpu", "memory", "gpu"} {
				if v, ok := parsed[key]; ok {
					if s, ok := v.(string); ok && s != "" {
						parts = append(parts, fmt.Sprintf("%s: %s", key, s))
					}
				}
			}
			if len(parts) > 0 {
				return fmt.Sprintf("✓ get_hardware_info: %s", strings.Join(parts, " · "))
			}
		}

	case "web_search":
		// Results come as pre-formatted text, not JSON
		return "✓ web_search done"
	}

	// Default: show tool name + first 3 lines if non-empty
	header := fmt.Sprintf("✓ %s done", name)
	trimmed := strings.TrimSpace(result)
	if trimmed != "" {
		lines, _ := firstNLines(trimmed, 3)
		body := prefixLines(lines, "│ ")
		return capOutput(header + "\n" + body)
	}
	return header
}
