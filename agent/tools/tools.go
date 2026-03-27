package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ZeroSegFault/linx/agent/providers"
	"github.com/ZeroSegFault/linx/backup"
	"github.com/ZeroSegFault/linx/config"
)

// validPackageName matches valid package name characters across distros.
var validPackageName = regexp.MustCompile(`^[a-zA-Z0-9._+@-]+$`)

// validatePackageNames splits the input on whitespace and validates each package name.
func validatePackageNames(input string) ([]string, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return nil, fmt.Errorf("no package names provided")
	}
	for _, name := range fields {
		if !validPackageName.MatchString(name) {
			return nil, fmt.Errorf("invalid package name %q: contains disallowed characters", name)
		}
	}
	return fields, nil
}

// Tool represents an executable tool the agent can call.
type Tool struct {
	Definition providers.ToolDefinition
	Execute    func(args json.RawMessage) (string, error)
	// RequiresConfirmation indicates the user must approve before execution.
	RequiresConfirmation bool
}

// ConfirmFunc is called to ask the user for confirmation. Returns true if approved.
type ConfirmFunc func(description string) bool

// Registry holds all registered tools.
type Registry struct {
	tools            map[string]*Tool
	confirm          ConfirmFunc
	packageManager   string
	maxCommandOutput int
	maxFileRead      int
	maxFetchChars    int
	maxManpageChars  int
	autoBackup       bool
}

// NewRegistry creates a tool registry with all core tools.
func NewRegistry(confirm ConfirmFunc, toolsCfg *config.ToolsConfig, enableManpages bool, autoBackup bool) *Registry {
	r := &Registry{
		tools:            make(map[string]*Tool),
		confirm:          confirm,
		packageManager:   detectPackageManager(),
		maxCommandOutput: toolsCfg.MaxCommandOutput,
		maxFileRead:      toolsCfg.MaxFileRead,
		maxFetchChars:    toolsCfg.MaxFetchChars,
		maxManpageChars:  toolsCfg.MaxManpageChars,
		autoBackup:       autoBackup,
	}
	if r.maxCommandOutput <= 0 {
		r.maxCommandOutput = 1048576 // 1MB default
	}
	if r.maxFileRead <= 0 {
		r.maxFileRead = 51200 // 50KB default
	}
	if r.maxFetchChars <= 0 {
		r.maxFetchChars = 8000
	}
	if r.maxManpageChars <= 0 {
		r.maxManpageChars = 8000
	}
	r.registerAll()
	r.RegisterWebTools(toolsCfg.BraveAPIKey)
	if enableManpages {
		r.register(r.lookupManpage())
	}
	return r
}

// Definitions returns all tool definitions for the LLM.
func (r *Registry) Definitions() []providers.ToolDefinition {
	defs := make([]providers.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition)
	}
	return defs
}

// Execute runs a tool by name with the given JSON arguments.
func (r *Registry) Execute(name string, args json.RawMessage) (string, error) {
	tool, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	if tool.RequiresConfirmation && r.confirm != nil {
		desc := formatConfirmation(name, args)
		if !r.confirm(desc) {
			return "User denied this action.", nil
		}
	}

	return tool.Execute(args)
}

// formatConfirmation creates a human-readable confirmation prompt.
func formatConfirmation(name string, args json.RawMessage) string {
	switch name {
	case "run_command_privileged":
		var params struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(args, &params) == nil {
			return fmt.Sprintf("Run with sudo: %s", params.Command)
		}
	case "write_file":
		var params struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if json.Unmarshal(args, &params) == nil {
			lines := strings.Count(params.Content, "\n") + 1
			return fmt.Sprintf("Write %d lines to: %s", lines, params.Path)
		}
	}
	return fmt.Sprintf("Execute tool %q with args: %s", name, string(args))
}

func (r *Registry) register(t *Tool) {
	r.tools[t.Definition.Name] = t
}

func (r *Registry) registerAll() {
	r.register(r.getOSInfo())
	r.register(r.readFile())
	r.register(r.runCommand())
	r.register(r.runCommandPrivileged())
	r.register(r.listPackages())
	r.register(r.getServiceStatus())
	r.register(r.readJournal())
	r.register(r.writeFile())
	r.register(r.getHardwareInfo())
	r.register(r.manageService())
	r.register(r.installPackage())
	r.register(r.removePackage())
}

// --- Tool implementations ---

func (r *Registry) getOSInfo() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "get_os_info",
			Description: "Get operating system information including distro, kernel version, desktop environment, and init system.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		Execute: func(_ json.RawMessage) (string, error) {
			info := make(map[string]string)

			// Read /etc/os-release
			if data, err := os.ReadFile("/etc/os-release"); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						key := parts[0]
						val := strings.Trim(parts[1], "\"")
						switch key {
						case "PRETTY_NAME":
							info["distro"] = val
						case "ID":
							info["distro_id"] = val
						case "VERSION_ID":
							info["version"] = val
						}
					}
				}
			}

			// Kernel
			if out, err := exec.Command("uname", "-r").Output(); err == nil {
				info["kernel"] = strings.TrimSpace(string(out))
			}

			// Architecture
			if out, err := exec.Command("uname", "-m").Output(); err == nil {
				info["arch"] = strings.TrimSpace(string(out))
			}

			// Hostname
			if out, err := exec.Command("hostname").Output(); err == nil {
				info["hostname"] = strings.TrimSpace(string(out))
			}

			// Init system
			if _, err := exec.LookPath("systemctl"); err == nil {
				info["init_system"] = "systemd"
			} else {
				info["init_system"] = "unknown"
			}

			// Desktop environment
			if de := os.Getenv("XDG_CURRENT_DESKTOP"); de != "" {
				info["desktop"] = de
			} else if de := os.Getenv("DESKTOP_SESSION"); de != "" {
				info["desktop"] = de
			} else {
				info["desktop"] = "none/headless"
			}

			// Package manager
			for _, pm := range []string{"pacman", "apt", "dnf", "zypper"} {
				if _, err := exec.LookPath(pm); err == nil {
					info["package_manager"] = pm
					break
				}
			}

			data, _ := json.MarshalIndent(info, "", "  ")
			return string(data), nil
		},
	}
}

func (r *Registry) readFile() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "read_file",
			Description: "Read the contents of a file at the given path. Note: This can read any file the current user has permission to access, including sensitive system files. Use responsibly.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute or relative file path to read",
					},
				},
				"required": []string{"path"},
			},
		},
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			params.Path = expandPath(params.Path)
			data, err := os.ReadFile(params.Path)
			if err != nil {
				return fmt.Sprintf("Error reading file: %v", err), nil
			}
			content := string(data)
			if len(content) > r.maxFileRead {
				content = content[:r.maxFileRead] + "\n\n[truncated — file exceeds size limit]"
			}
			return content, nil
		},
	}
}

func (r *Registry) runCommand() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "run_command",
			Description: "Run a shell command and return stdout, stderr, and exit code. Do NOT use for commands requiring sudo — use run_command_privileged instead.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The shell command to execute",
					},
				},
				"required": []string{"command"},
			},
		},
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			return r.executeShell(params.Command, false)
		},
	}
}

func (r *Registry) runCommandPrivileged() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "run_command_privileged",
			Description: "Run a shell command with sudo (root privileges). The user will be shown the command and asked to confirm before execution.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The shell command to execute with sudo",
					},
				},
				"required": []string{"command"},
			},
		},
		RequiresConfirmation: true,
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			return r.executeShell(params.Command, true)
		},
	}
}

func (r *Registry) listPackages() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "list_packages",
			Description: "List installed packages, optionally filtering by a search term. Automatically detects the system's package manager (pacman, apt, dnf).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filter": map[string]interface{}{
						"type":        "string",
						"description": "Optional search/filter term to grep for in the package list",
					},
				},
			},
		},
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Filter string `json:"filter"`
			}
			_ = json.Unmarshal(args, &params)

			var cmd string
			switch r.packageManager {
			case "pacman":
				cmd = "pacman -Q"
			case "apt":
				cmd = "dpkg -l"
			case "dnf", "zypper":
				cmd = "rpm -qa"
			default:
				return "No supported package manager found (tried pacman, apt, dnf, zypper)", nil
			}

			if params.Filter != "" {
				cmd += " | grep -i " + shellescape(params.Filter)
			}

			return r.executeShell(cmd, false)
		},
	}
}

func (r *Registry) getServiceStatus() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "get_service_status",
			Description: "Get the systemctl status of a systemd service unit.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"unit": map[string]interface{}{
						"type":        "string",
						"description": "Name of the systemd unit (e.g., sshd, NetworkManager)",
					},
				},
				"required": []string{"unit"},
			},
		},
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Unit string `json:"unit"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			return r.executeShell("systemctl status "+shellescape(params.Unit)+" 2>&1 || true", false)
		},
	}
}

func (r *Registry) readJournal() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "read_journal",
			Description: "Read systemd journal (journalctl) output for a specific unit or recent entries.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"unit": map[string]interface{}{
						"type":        "string",
						"description": "Systemd unit to filter by (optional)",
					},
					"lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of recent lines to show (default 50)",
					},
					"priority": map[string]interface{}{
						"type":        "string",
						"description": "Minimum priority level (emerg, alert, crit, err, warning, notice, info, debug)",
					},
				},
			},
		},
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Unit     string `json:"unit"`
				Lines    int    `json:"lines"`
				Priority string `json:"priority"`
			}
			_ = json.Unmarshal(args, &params)

			if params.Lines <= 0 {
				params.Lines = 50
			}

			cmd := fmt.Sprintf("journalctl --no-pager -n %d", params.Lines)
			if params.Unit != "" {
				cmd += " -u " + shellescape(params.Unit)
			}
			if params.Priority != "" {
				cmd += " -p " + shellescape(params.Priority)
			}

			return r.executeShell(cmd, false)
		},
	}
}

func (r *Registry) writeFile() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "write_file",
			Description: "Write content to a file. If the file already exists, a backup is created automatically before overwriting. User confirmation is required.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute or relative file path to write",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		RequiresConfirmation: true,
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			params.Path = expandPath(params.Path)

			// Auto-backup if file exists and auto_backup is enabled
			perm := os.FileMode(0o644) // default for new files
			if r.autoBackup {
				if info, err := os.Stat(params.Path); err == nil {
					perm = info.Mode()
					idx, loadErr := backup.LoadIndex()
					if loadErr != nil {
						return fmt.Sprintf("Failed to load backup index: %v", loadErr), nil
					}
					_, backupErr := idx.BackupFile(params.Path, "", "auto-backup before write_file")
					if backupErr != nil {
						return fmt.Sprintf("Failed to create backup: %v", backupErr), nil
					}
					if saveErr := idx.Save(); saveErr != nil {
						return fmt.Sprintf("Failed to save backup index: %v", saveErr), nil
					}
				}
			} else {
				// Still get permissions for existing files
				if info, err := os.Stat(params.Path); err == nil {
					perm = info.Mode()
				}
			}

			dir := filepath.Dir(params.Path)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Sprintf("Failed to create directory %s: %v", dir, err), nil
			}

			if err := os.WriteFile(params.Path, []byte(params.Content), perm); err != nil {
				return fmt.Sprintf("Failed to write file: %v", err), nil
			}

			return fmt.Sprintf("Successfully wrote %d bytes to %s", len(params.Content), params.Path), nil
		},
	}
}

// --- New tool implementations ---

func (r *Registry) getHardwareInfo() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "get_hardware_info",
			Description: "Get hardware information including PCI devices, block devices, and memory usage.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		Execute: func(_ json.RawMessage) (string, error) {
			var sb strings.Builder

			sb.WriteString("=== PCI Devices ===\n")
			if out, err := exec.Command("lspci").CombinedOutput(); err == nil {
				sb.WriteString(strings.TrimSpace(string(out)))
			} else {
				sb.WriteString("lspci not available")
			}

			sb.WriteString("\n\n=== Block Devices ===\n")
			if out, err := exec.Command("lsblk").CombinedOutput(); err == nil {
				sb.WriteString(strings.TrimSpace(string(out)))
			} else {
				sb.WriteString("lsblk not available")
			}

			sb.WriteString("\n\n=== Memory ===\n")
			if out, err := exec.Command("free", "-h").CombinedOutput(); err == nil {
				sb.WriteString(strings.TrimSpace(string(out)))
			} else {
				sb.WriteString("free not available")
			}

			return sb.String(), nil
		},
	}
}

func (r *Registry) manageService() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "manage_service",
			Description: "Enable, disable, start, stop, or restart a systemd service unit. Requires user confirmation.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"unit": map[string]interface{}{
						"type":        "string",
						"description": "Name of the systemd unit (e.g., sshd, NetworkManager)",
					},
					"action": map[string]interface{}{
						"type":        "string",
						"description": "Action to perform: enable, disable, start, stop, restart",
						"enum":        []string{"enable", "disable", "start", "stop", "restart"},
					},
				},
				"required": []string{"unit", "action"},
			},
		},
		RequiresConfirmation: true,
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Unit   string `json:"unit"`
				Action string `json:"action"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}

			validActions := map[string]bool{"enable": true, "disable": true, "start": true, "stop": true, "restart": true}
			if !validActions[params.Action] {
				return fmt.Sprintf("Invalid action %q. Must be one of: enable, disable, start, stop, restart", params.Action), nil
			}

			cmd := fmt.Sprintf("systemctl %s %s", params.Action, shellescape(params.Unit))
			return r.executeShell(cmd, true)
		},
	}
}

func detectPackageManager() string {
	for _, pm := range []string{"pacman", "apt", "dnf", "zypper"} {
		if _, err := exec.LookPath(pm); err == nil {
			return pm
		}
	}
	return ""
}

func (r *Registry) installPackage() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "install_package",
			Description: "Install packages using the system package manager (auto-detects pacman/apt/dnf/zypper). Requires user confirmation.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"packages": map[string]interface{}{
						"type":        "string",
						"description": "Space-separated package names to install",
					},
				},
				"required": []string{"packages"},
			},
		},
		RequiresConfirmation: true,
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Packages string `json:"packages"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}

			names, err := validatePackageNames(params.Packages)
			if err != nil {
				return fmt.Sprintf("Invalid package names: %v", err), nil
			}

			if r.packageManager == "" {
				return "No supported package manager found (tried pacman, apt, dnf, zypper)", nil
			}

			escaped := make([]string, len(names))
			for i, n := range names {
				escaped[i] = shellescape(n)
			}
			pkgs := strings.Join(escaped, " ")

			var cmd string
			switch r.packageManager {
			case "pacman":
				cmd = "pacman -S --noconfirm " + pkgs
			case "apt":
				cmd = "apt install -y " + pkgs
			case "dnf":
				cmd = "dnf install -y " + pkgs
			case "zypper":
				cmd = "zypper install -y " + pkgs
			}

			return r.executeShell(cmd, true)
		},
	}
}

func (r *Registry) removePackage() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "remove_package",
			Description: "Remove packages using the system package manager (auto-detects pacman/apt/dnf/zypper). Requires user confirmation.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"packages": map[string]interface{}{
						"type":        "string",
						"description": "Space-separated package names to remove",
					},
				},
				"required": []string{"packages"},
			},
		},
		RequiresConfirmation: true,
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Packages string `json:"packages"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}

			names, err := validatePackageNames(params.Packages)
			if err != nil {
				return fmt.Sprintf("Invalid package names: %v", err), nil
			}

			if r.packageManager == "" {
				return "No supported package manager found (tried pacman, apt, dnf, zypper)", nil
			}

			escaped := make([]string, len(names))
			for i, n := range names {
				escaped[i] = shellescape(n)
			}
			pkgs := strings.Join(escaped, " ")

			var cmd string
			switch r.packageManager {
			case "pacman":
				cmd = "pacman -R --noconfirm " + pkgs
			case "apt":
				cmd = "apt remove -y " + pkgs
			case "dnf":
				cmd = "dnf remove -y " + pkgs
			case "zypper":
				cmd = "zypper remove -y " + pkgs
			}

			return r.executeShell(cmd, true)
		},
	}
}

func (r *Registry) lookupManpage() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "lookup_manpage",
			Description: "Look up a man page for a command or topic. Returns the relevant documentation. Use this before running unfamiliar commands to check correct flags and syntax.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The command name (e.g., \"pacman\", \"systemctl\", \"journalctl\")",
					},
					"section": map[string]interface{}{
						"type":        "string",
						"description": "Man page section number (e.g., \"1\", \"5\", \"8\"). Optional.",
					},
				},
				"required": []string{"command"},
			},
		},
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				Command string `json:"command"`
				Section string `json:"section"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			if params.Command == "" {
				return "Error: command parameter is required", nil
			}

			// Build man command
			var manArgs []string
			if params.Section != "" {
				manArgs = []string{params.Section, params.Command}
			} else {
				manArgs = []string{params.Command}
			}

			// Run man <section> <command> 2>/dev/null | col -bx
			manCmd := exec.Command("man", manArgs...)
			manOut, err := manCmd.Output()
			if err == nil {
				// Strip formatting with col -bx
				colCmd := exec.Command("col", "-bx")
				colCmd.Stdin = strings.NewReader(string(manOut))
				colOut, colErr := colCmd.Output()
				var result string
				if colErr == nil {
					result = string(colOut)
				} else {
					result = string(manOut)
				}

				runes := []rune(result)
				if len(runes) > r.maxManpageChars {
					result = string(runes[:r.maxManpageChars]) + fmt.Sprintf("\n\n[truncated — man page exceeds %d chars]", r.maxManpageChars)
				}
				return result, nil
			}

			// Fallback: try --help
			helpCmd := exec.Command(params.Command, "--help")
			helpOut, helpErr := helpCmd.CombinedOutput()
			if helpErr == nil || len(helpOut) > 0 {
				result := string(helpOut)
				runes := []rune(result)
				if len(runes) > r.maxManpageChars {
					result = string(runes[:r.maxManpageChars]) + fmt.Sprintf("\n\n[truncated — help output exceeds %d chars]", r.maxManpageChars)
				}
				if result != "" {
					return fmt.Sprintf("(man page not found, showing --help output)\n\n%s", result), nil
				}
			}

			return fmt.Sprintf("No man page or --help output found for %q", params.Command), nil
		},
	}
}

// --- Helpers ---

func (r *Registry) executeShell(command string, sudo bool) (string, error) {
	if sudo && os.Getuid() != 0 {
		command = "sudo " + command
	}

	// If the command ends with &, it's trying to background a process.
	// Redirect stdout/stderr to /dev/null and use nohup to prevent the
	// backgrounded process from inheriting pipes (which would cause
	// CombinedOutput to hang forever).
	trimmedCmd := strings.TrimSpace(command)
	if strings.HasSuffix(trimmedCmd, "&") {
		// Wrap: nohup <cmd without &> > /dev/null 2>&1 &
		baseCmd := strings.TrimSuffix(trimmedCmd, "&")
		baseCmd = strings.TrimSpace(baseCmd)
		command = fmt.Sprintf("nohup %s > /dev/null 2>&1 &", baseCmd)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	output, err := cmd.CombinedOutput()

	truncated := false
	if len(output) > r.maxCommandOutput {
		output = output[:r.maxCommandOutput]
		truncated = true
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", fmt.Errorf("failed to execute command: %w", err)
		}
	}

	outStr := strings.TrimSpace(string(output))
	if truncated {
		outStr += "\n\n[truncated — output exceeds limit]"
	}

	result := map[string]interface{}{
		"command":   command,
		"output":    outStr,
		"exit_code": exitCode,
	}
	if truncated {
		result["truncated"] = true
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// expandPath expands ~ and ~user prefixes to absolute paths.
func expandPath(path string) string {
	if path == "" {
		return path
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
