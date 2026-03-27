package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds all Linx configuration.
type Config struct {
	DefaultProfile string                    `toml:"default_profile"`
	Profiles       map[string]ProviderConfig `toml:"profiles"`
	Provider       ProviderConfig            `toml:"provider"`  // legacy, backward compat
	Behavior       BehaviorConfig            `toml:"behavior"`
	Tools          ToolsConfig               `toml:"tools"`
	Warnings       []string                  `toml:"-"` // populated by Load(), not serialized
}

// ProviderConfig configures the LLM provider.
type ProviderConfig struct {
	Type          string `toml:"type"`           // openai | codex | ollama | anthropic
	BaseURL       string `toml:"base_url"`       // API base URL
	APIKey        string `toml:"api_key"`        // API key (openai/anthropic)
	Model         string `toml:"model"`          // model name
	TimeoutSec    int    `toml:"timeout_sec"`    // API timeout in seconds (default 600)
	ContextWindow int    `toml:"context_window"` // tokens, default 32000
}

// BehaviorConfig controls agent behavior.
type BehaviorConfig struct {
	ConfirmDestructive bool `toml:"confirm_destructive"`
	AutoBackup         bool `toml:"auto_backup"`
	PasswordlessSudo   bool `toml:"passwordless_sudo"`
	RequireResearch    bool `toml:"require_research"`
	EnableManpages     bool `toml:"enable_manpages"`
	MaxBackups         int  `toml:"max_backups,omitempty"`
	MaxToolsPerRound   int  `toml:"max_tools_per_round,omitempty"`
	MaxToolRounds      int  `toml:"max_tool_rounds,omitempty"`
	CompactThreshold   int  `toml:"compact_threshold,omitempty"` // % before auto-compact, default 90
}

// ToolsConfig configures optional agent tools.
type ToolsConfig struct {
	BraveAPIKey      string `toml:"brave_api_key"`      // Brave Search API key (empty = disabled)
	MaxCommandOutput int    `toml:"max_command_output"` // Max command output in bytes (default 1MB)
	MaxFileRead      int    `toml:"max_file_read"`      // Max file read size in bytes (default 50KB)
	MaxFetchChars    int    `toml:"max_fetch_chars"`    // Max chars from fetched URLs (default 8000)
	MaxManpageChars  int    `toml:"max_manpage_chars"`  // Max chars from man pages (default 8000)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Provider: ProviderConfig{
			Type:    "openai",
			BaseURL: "https://api.openai.com/v1",
			APIKey:  "",
			Model:   "gpt-4o-mini",
		},
		Behavior: BehaviorConfig{
			ConfirmDestructive: true,
			AutoBackup:         true,
			PasswordlessSudo:   false,
			RequireResearch:    true,
			EnableManpages:     true,
			MaxToolsPerRound:   10,
		},
	}
}

// configDir returns the config directory, respecting XDG_CONFIG_HOME.
func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "linx")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "linx")
}

// DefaultPath returns the default config file path.
func DefaultPath() string {
	return filepath.Join(configDir(), "config.toml")
}

// SecretsPath returns the path to the secrets file.
func SecretsPath() string {
	return filepath.Join(configDir(), "secrets.toml")
}

// Secrets holds API keys loaded from secrets.toml.
type Secrets struct {
	Profiles map[string]ProviderSecrets `toml:"profiles"`
	Provider ProviderSecrets            `toml:"provider"`
	Tools    ToolsSecrets               `toml:"tools"`
}

// ProviderSecrets holds just the API key for a provider.
type ProviderSecrets struct {
	APIKey string `toml:"api_key"`
}

// ToolsSecrets holds API keys for tools.
type ToolsSecrets struct {
	BraveAPIKey string `toml:"brave_api_key"`
}

// loadSecrets reads and parses the secrets file. Returns nil, nil if absent.
func loadSecrets() (*Secrets, error) {
	path := SecretsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading secrets %s: %w", path, err)
	}

	var secrets Secrets
	if err := toml.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("parsing secrets %s: %w", path, err)
	}
	return &secrets, nil
}

// mergeSecrets applies secrets on top of the loaded config (secrets win).
func mergeSecrets(cfg *Config, secrets *Secrets) {
	if secrets == nil {
		return
	}

	// Merge legacy provider key
	if secrets.Provider.APIKey != "" {
		cfg.Provider.APIKey = secrets.Provider.APIKey
	}

	// Merge profile keys
	for name, ps := range secrets.Profiles {
		if ps.APIKey != "" {
			if p, ok := cfg.Profiles[name]; ok {
				p.APIKey = ps.APIKey
				cfg.Profiles[name] = p
			} else {
				// Profile exists in secrets but not config — create partial entry
				if cfg.Profiles == nil {
					cfg.Profiles = make(map[string]ProviderConfig)
				}
				cfg.Profiles[name] = ProviderConfig{APIKey: ps.APIKey}
			}
		}
	}

	// Merge tool keys
	if secrets.Tools.BraveAPIKey != "" {
		cfg.Tools.BraveAPIKey = secrets.Tools.BraveAPIKey
	}
}

const defaultSecretsTemplate = `# Linx secrets — API keys and tokens
# This file is auto-created with restricted permissions (0600)
# Keys here override those in config.toml
#
# [profiles.openai]
# api_key = "sk-your-key-here"
#
# [tools]
# brave_api_key = "your-brave-key"
`

func saveSecretsTemplate(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultSecretsTemplate), 0o600)
}

// defaultConfigTemplate is the TOML written when no config file exists.
const defaultConfigTemplate = `# Linx configuration
# See: https://github.com/ZeroSegFault/linx

# Provider profiles — define multiple providers and switch with --profile
# Uncomment and customize:
#
# default_profile = "local"
#
# [profiles.local]
# type = "openai"
# base_url = "http://localhost:8000/v1"
# api_key = "not-needed"
# model = "qwen3.5-122b-a10b"
#
# [profiles.cloud]
# type = "openai"
# base_url = "https://api.openai.com/v1"
# api_key = "your-key-here"
# model = "gpt-5.4"

# Single provider (used when no profiles are defined)
[provider]
# Provider type: openai | codex | ollama
type = "openai"

# API base URL
# OpenAI:    https://api.openai.com/v1
# Ollama:    http://localhost:11434
# LM Studio: http://localhost:1234/v1
# Any OpenAI-compatible server works
base_url = "https://api.openai.com/v1"

# API key (can also be set in secrets.toml for better security)
api_key = ""

# Model name
# OpenAI: gpt-4o-mini, gpt-4o, gpt-4.1, gpt-5.4
# Ollama: llama3.2, qwen3, etc.
model = "gpt-4o-mini"

# API timeout in seconds (default 600 = 10 minutes)
# timeout_sec = 600

# Context window size in tokens (default 32000, used for session management)
# context_window = 32000

[behavior]
# Ask for confirmation before destructive operations (file writes, sudo, package changes)
confirm_destructive = true

# Auto-backup files before overwriting them
auto_backup = true

# If true, assume NOPASSWD sudo is configured (skip password prompts)
passwordless_sudo = false

# Require the agent to research (read files, check man pages, search) before destructive actions
require_research = true

# Enable the lookup_manpage tool (lets the agent read man pages for command syntax)
enable_manpages = true

# Maximum tool calls the agent can make per LLM response (default 10)
# max_tools_per_round = 10

# Maximum agent loop rounds before giving up (default 50)
# max_tool_rounds = 50

[tools]
# Brave Search API key (can also be set in secrets.toml)
# Get a free key at https://brave.com/search/api/
brave_api_key = ""

# Maximum command output size in bytes (default 1MB)
# max_command_output = 1048576

# Maximum file read size in bytes (default 50KB)
# max_file_read = 51200

# Maximum characters from fetched URLs (default 8000)
# max_fetch_chars = 8000

# Maximum characters from man pages (default 8000)
# max_manpage_chars = 8000
`

// Validate returns a list of configuration warnings (non-fatal issues).
func (c *Config) Validate() []string {
	var warnings []string

	// Check provider type
	switch c.Provider.Type {
	case "openai", "codex", "ollama":
		// valid
	default:
		warnings = append(warnings, fmt.Sprintf("unknown provider type %q (expected openai, codex, or ollama)", c.Provider.Type))
	}

	// Check model
	if c.Provider.Model == "" {
		warnings = append(warnings, "provider.model is empty")
	}

	// Check API key for openai
	if c.Provider.Type == "openai" && c.Provider.APIKey == "" {
		warnings = append(warnings, "provider.api_key is empty for openai provider")
	}

	// Warn if a real API key is in config.toml (should be in secrets.toml)
	if c.Provider.APIKey != "" && strings.HasPrefix(c.Provider.APIKey, "sk-") {
		warnings = append(warnings, "API key found in config.toml — consider moving it to secrets.toml for better security")
	}

	// Validate profiles
	for name, p := range c.Profiles {
		switch p.Type {
		case "openai", "codex", "ollama", "":
			// valid (empty means partially configured, e.g. from secrets only)
		default:
			warnings = append(warnings, fmt.Sprintf("profile %q: unknown provider type %q", name, p.Type))
		}
		if p.Type != "" && p.Model == "" {
			warnings = append(warnings, fmt.Sprintf("profile %q: model is empty", name))
		}
		if p.Type == "openai" && p.APIKey == "" {
			warnings = append(warnings, fmt.Sprintf("profile %q: api_key is empty for openai provider", name))
		}
		if p.APIKey != "" && strings.HasPrefix(p.APIKey, "sk-") {
			warnings = append(warnings, fmt.Sprintf("profile %q: API key in config.toml — consider moving to secrets.toml", name))
		}
	}

	// Check max_backups
	if c.Behavior.MaxBackups != 0 && c.Behavior.MaxBackups < 1 {
		warnings = append(warnings, fmt.Sprintf("behavior.max_backups is %d (must be >= 1 if set)", c.Behavior.MaxBackups))
	}

	return warnings
}

// ResolveProvider returns the active ProviderConfig based on the selected profile.
// If profileName is empty, uses DefaultProfile. If DefaultProfile is empty, falls back to legacy [provider].
func (c *Config) ResolveProvider(profileName string) (*ProviderConfig, error) {
	// If a specific profile is requested
	if profileName != "" {
		p, ok := c.Profiles[profileName]
		if !ok {
			available := make([]string, 0, len(c.Profiles))
			for k := range c.Profiles {
				available = append(available, k)
			}
			sort.Strings(available)
			return nil, fmt.Errorf("unknown profile %q (available: %s)", profileName, strings.Join(available, ", "))
		}
		return &p, nil
	}

	// Use default profile
	if c.DefaultProfile != "" {
		p, ok := c.Profiles[c.DefaultProfile]
		if !ok {
			return nil, fmt.Errorf("default profile %q not found in [profiles]", c.DefaultProfile)
		}
		return &p, nil
	}

	// Legacy fallback: use [provider] section
	if c.Provider.Type != "" {
		return &c.Provider, nil
	}

	// No profiles and no legacy provider
	if len(c.Profiles) > 0 {
		// Use the first profile alphabetically
		names := make([]string, 0, len(c.Profiles))
		for k := range c.Profiles {
			names = append(names, k)
		}
		sort.Strings(names)
		p := c.Profiles[names[0]]
		return &p, nil
	}

	return nil, fmt.Errorf("no provider configured — add [profiles] or [provider] to config")
}

// ListProfiles returns the names of all configured profiles, sorted alphabetically.
func (c *Config) ListProfiles() []string {
	if len(c.Profiles) == 0 {
		if c.Provider.Type != "" {
			return []string{"default"}
		}
		return nil
	}
	names := make([]string, 0, len(c.Profiles))
	for k := range c.Profiles {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Load reads the config from disk, falling back to defaults.
// If path is empty, the default path is used.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file — save template and use defaults
			_ = saveTemplate(path)
			_ = saveSecretsTemplate(SecretsPath())
			cfg.Warnings = cfg.Validate()
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Load and merge secrets
	secrets, err := loadSecrets()
	if err != nil {
		cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("could not load secrets: %v", err))
	} else {
		mergeSecrets(cfg, secrets)
	}

	cfg.Warnings = append(cfg.Warnings, cfg.Validate()...)

	return cfg, nil
}

// saveTemplate writes the commented config template to disk.
func saveTemplate(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir %s: %w", dir, err)
	}
	return os.WriteFile(path, []byte(defaultConfigTemplate), 0o644)
}

// Save writes the config to disk at the given path.
func Save(cfg *Config, path string) error {
	if path == "" {
		path = DefaultPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir %s: %w", dir, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating config file %s: %w", path, err)
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	return enc.Encode(cfg)
}

// Print writes the config in TOML format to the given writer.
func Print(cfg *Config, w io.Writer) error {
	enc := toml.NewEncoder(w)
	return enc.Encode(cfg)
}
