# 🐧 Linx

**AI-powered Linux system assistant** — configure, troubleshoot, and manage your Linux systems from the terminal.

![Build](https://img.shields.io/badge/build-passing-brightgreen)
![Version](https://img.shields.io/badge/version-0.20.0-blue)
![License](https://img.shields.io/badge/license-AGPL--3.0-purple)

Linx (`lx`) is a CLI/TUI tool written in Go that uses LLM backends to help with Linux system administration. It can inspect your system, read logs, manage services, install packages, edit config files, search the web — all with intelligent context, automatic backups, and persistent memory.

## Features

- **CLI mode** — pass a prompt as bare words, no quotes needed (`lx fix my wifi`)
- **Interactive TUI** — Bubble Tea interface with live streaming output, spinner, and confirmation prompts
- **Streaming LLM output** — tokens appear as they arrive from the LLM
- **15 built-in tools** — system inspection, file management, package ops, services, web search, man pages
- **Research gate** — forces the agent to gather information before destructive operations (configurable)
- **Confirmation gate** — destructive operations require explicit user approval (configurable)
- **Auto-backup & rollback** — files are backed up before modification, with interactive restore
- **Persistent memory** — learns about your system across sessions via LLM-powered extraction
- **Session history logs** — daily conversation logs for audit and reference
- **Named provider profiles** — switch between LLM providers with `--profile`
- **Per-invocation model override** — use `--model` to try a different model without changing config
- **Separate secrets.toml** — API keys in a dedicated file with 0600 permissions
- **Configurable limits** — command output, file read, URL fetch, man page, and tools-per-round limits
- **Config validation** — warns about common misconfigurations
- **Doctor health check** — verify config, provider connectivity, memory, backups, and auth status
- **OAuth PKCE login** — authenticate with Codex provider via ChatGPT browser flow
- **Backup index pruning** — keeps the last 100 entries to prevent unbounded growth
- **Package name validation** — protects against shell injection in package operations
- **Rune-safe truncation** — all output truncation is Unicode-aware
- **API call timeouts** — 3-minute timeout on all LLM API calls

## Quick Start

Requires Go 1.22+:

```bash
git clone https://github.com/ZeroSegFault/linx.git
cd linx
make build
```

The binary will be at `./lx`. To install system-wide:

```bash
sudo make install    # installs to /usr/local/bin/lx
sudo make uninstall  # removes it
```

Other Makefile targets:

```bash
make test     # run all tests
make clean    # remove built binary
make version  # print current version
```

## Usage

### TUI mode (interactive)

```bash
lx
```

Opens a rich terminal interface powered by Bubble Tea. Type questions or commands, press Enter to submit.

**Keyboard shortcuts:**
- `Enter` — submit prompt
- `Ctrl+L` — clear screen
- `Ctrl+C` — quit

### CLI mode (single-shot)

```bash
lx fix my wifi                     # bare words — no quotes needed
lx "configure my display"          # quotes work too
lx check if sshd is running        # natural language
```

### All flags

```
lx                                    Interactive TUI mode
lx fix my wifi                        CLI prompt (bare words)
lx "configure my display"             CLI prompt (quoted)
lx --model gpt-5.4 "fix wifi"         Model override for this invocation
lx --profile cloud "fix wifi"         Use a named provider profile
lx --profiles                         List configured profiles
lx --status                           Auth status for all providers
lx --login --provider codex           OAuth PKCE login (opens browser)
lx --logout --provider codex          Clear stored credentials
lx --models                           List available models from provider
lx --doctor                           Health check
lx --version                          Version and build info
lx --show-config                      Print current config and exit
lx --config /path/to/config.toml      Use a custom config file
lx --rollback                         List and restore backups (last 20)
lx --rollback --last 5                Show last 5 backups
lx --memory                           Show persistent memory
lx --memory --edit                    Open memory in $EDITOR
lx --memory --clear                   Clear all memory (with confirmation)
```

## Configuration

Config lives at `~/.config/linx/config.toml` (respects `XDG_CONFIG_HOME`). A default config with full comments is created on first run.

### Provider Profiles

Linx supports named provider profiles. Define multiple providers and switch between them:

```toml
default_profile = "local"

[profiles.local]
type = "openai"
base_url = "http://localhost:8000/v1"
api_key = "not-needed"
model = "qwen3.5-122b-a10b"

[profiles.cloud]
type = "openai"
base_url = "https://api.openai.com/v1"
api_key = ""  # use secrets.toml
model = "gpt-5.4"
```

Switch profiles per invocation:

```bash
lx --profile cloud "fix my wifi"
lx --profiles  # list all configured profiles
```

If no profiles are defined, Linx falls back to the legacy `[provider]` section.

### Legacy `[provider]` section

Used when no `[profiles]` are defined:

```toml
[provider]
type = "openai"          # openai | codex | ollama
base_url = "https://api.openai.com/v1"
api_key = ""
model = "gpt-4o-mini"
```

| Field      | Type   | Description                                                               |
|------------|--------|---------------------------------------------------------------------------|
| `type`     | string | Provider type: `openai`, `codex`, `ollama` (any OpenAI-compatible works)  |
| `base_url` | string | API base URL (e.g., `https://api.openai.com/v1`, `http://localhost:11434`) |
| `api_key`  | string | API key (not needed for Ollama/local servers; prefer secrets.toml)        |
| `model`    | string | Model name (e.g., `gpt-4o-mini`, `codex-mini-latest`, `llama3.2`)        |

### `[behavior]`

```toml
[behavior]
confirm_destructive = true    # ask before destructive operations
auto_backup = true            # back up files before overwriting
passwordless_sudo = false     # assume NOPASSWD sudo is configured
require_research = true       # force research before destructive ops
enable_manpages = true        # enable the lookup_manpage tool
max_tools_per_round = 10      # max tool calls per LLM response
```

| Field                 | Type | Default | Description                                                          |
|-----------------------|------|---------|----------------------------------------------------------------------|
| `confirm_destructive` | bool | `true`  | Require user confirmation for file writes, sudo, package changes     |
| `auto_backup`         | bool | `true`  | Automatically back up files before `write_file` overwrites them      |
| `passwordless_sudo`   | bool | `false` | Skip sudo password prompts (set `true` if NOPASSWD is configured)   |
| `require_research`    | bool | `true`  | Block destructive tools until a research tool has been called first  |
| `enable_manpages`     | bool | `true`  | Register the `lookup_manpage` tool (lets the agent read man pages)   |
| `max_tools_per_round` | int  | `10`    | Maximum tool calls the agent can make per LLM response               |

### `[tools]`

```toml
[tools]
brave_api_key = ""            # Brave Search API key (empty = web search disabled)
max_command_output = 1048576  # max command output in bytes (default 1MB)
max_file_read = 51200         # max file read size in bytes (default 50KB)
max_fetch_chars = 8000        # max chars from fetched URLs
max_manpage_chars = 8000      # max chars from man pages
```

| Field                | Type   | Default   | Description                                                   |
|----------------------|--------|-----------|---------------------------------------------------------------|
| `brave_api_key`      | string | `""`      | Brave Search API key — get one free at https://brave.com/search/api/ |
| `max_command_output` | int    | `1048576` | Maximum bytes of command output returned to the LLM (1 MB)    |
| `max_file_read`      | int    | `51200`   | Maximum bytes when reading files (50 KB)                      |
| `max_fetch_chars`    | int    | `8000`    | Maximum characters from fetched URLs                          |
| `max_manpage_chars`  | int    | `8000`    | Maximum characters from man page lookups                      |

## Secrets

API keys can (and should) be stored separately in `~/.config/linx/secrets.toml`, which is created with `0600` permissions on first run.

```toml
# Keys here override those in config.toml

[profiles.cloud]
api_key = "sk-your-openai-key"

[provider]
api_key = "sk-legacy-key"

[tools]
brave_api_key = "your-brave-key"
```

**Auto-merge behavior:** When Linx loads config, it reads `secrets.toml` and merges any keys on top of `config.toml`. Secrets always win — if a key exists in both files, the secrets.toml value is used.

This lets you keep `config.toml` in version control or share it without leaking API keys.

## Tools Reference

Linx provides 15 built-in tools to the LLM:

| Tool                     | Description                                                                    |
|--------------------------|--------------------------------------------------------------------------------|
| `get_os_info`            | Get OS info — distro, kernel, architecture, hostname, desktop, init system, package manager |
| `get_hardware_info`      | Get hardware info — PCI devices, block devices, memory usage                   |
| `read_file`              | Read a file's contents (respects `max_file_read` limit)                        |
| `write_file`             | Write content to a file — auto-backs up existing files, requires confirmation  |
| `run_command`            | Run a shell command (non-privileged)                                           |
| `run_command_privileged` | Run a shell command with sudo — requires user confirmation                     |
| `list_packages`          | List installed packages with optional filter (auto-detects pacman/apt/dnf/zypper) |
| `install_package`        | Install packages via system package manager — requires confirmation            |
| `remove_package`         | Remove packages via system package manager — requires confirmation             |
| `get_service_status`     | Get systemctl status of a systemd service                                      |
| `manage_service`         | Start, stop, restart, enable, or disable a systemd service — requires confirmation |
| `read_journal`           | Read systemd journal logs (filterable by unit, line count, priority)            |
| `web_search`             | Search the web via Brave Search API (requires API key)                         |
| `fetch_url`              | Fetch a URL and return readable text (HTML stripped, respects `max_fetch_chars`) |
| `lookup_manpage`         | Look up a man page for a command — falls back to `--help` (configurable)       |

## Safety Features

### Research Gate

When `require_research = true` (default), the agent **must** call at least one research tool before it can use destructive tools. Research tools include:

- `get_os_info`, `get_hardware_info`, `read_file`, `list_packages`
- `get_service_status`, `read_journal`, `lookup_manpage`
- `web_search`, `fetch_url`

If the agent tries to write a file, run a privileged command, install/remove a package, or manage a service without researching first, it gets a warning and must gather information first.

### Confirmation Gate

When `confirm_destructive = true` (default), the following tools require explicit user approval before execution:

- `write_file`, `run_command_privileged`
- `install_package`, `remove_package`, `manage_service`

The user sees exactly what will be executed and can approve or deny.

### Package Name Validation

All package names are validated against `^[a-zA-Z0-9._+@-]+$` before being passed to the package manager, preventing shell injection attacks.

### Tool Rate Limiting

The agent is limited to `max_tools_per_round` tool calls per LLM response (default 10). Excess calls are skipped with a warning. The overall agent loop is capped at 20 rounds.

## Memory System

Linx maintains persistent memory at `~/.local/share/linx/MEMORY.md` (respects `XDG_DATA_HOME`).

After each session, the LLM extracts durable facts from the conversation and updates the memory file. This includes:

- **System Profile** — distro, kernel, desktop environment, init system, package manager
- **User Preferences** — how you like things configured
- **Successful Changes** — what was changed and when
- **Known Issues** — problems encountered and their resolutions
- **Failed Approaches** — what was tried and didn't work

Memory is injected into the system prompt (up to 4000 characters) so the agent has context from previous sessions.

Session history is logged daily at `~/.local/share/linx/history/YYYY-MM-DD.md`.

```bash
lx --memory            # view current memory
lx --memory --edit     # open in $EDITOR
lx --memory --clear    # wipe (with confirmation)
```

## Sessions

Linx maintains persistent sessions in TUI mode. Each TUI session preserves conversation history across prompts.

### Session Management
```bash
lx --sessions              # List recent sessions
lx --sessions --last 20    # Show last 20 sessions
lx --resume a1b2           # Resume by UUID prefix
lx --resume 3              # Resume by list number
lx --sessions --clear      # Clear archived sessions
```

### Crash Recovery
If lx crashes or is killed, the next TUI launch will detect the orphaned session and offer to restore it.

### Context Window
The status bar shows context usage percentage. When context exceeds 90%, old conversation turns are automatically compacted to free space.

## Backup & Rollback

When Linx modifies a file via `write_file`, the original is automatically backed up to `~/.local/share/linx/backups/`. A backup index tracks all entries (pruned to the last 100).

```bash
lx --rollback          # interactive restore (last 20)
lx --rollback --last 5 # show last 5 backups
```

Select backups by number, confirm, and the original file is restored.

## License

Linx is licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**. See [LICENSE](LICENSE) for full text.

For commercial licensing options (embedding in proprietary software, SaaS, managed services), see [COMMERCIAL.md](COMMERCIAL.md).

Copyright © 2026 Ashley Stonham. All rights reserved.

## Contributing

Contributions are welcome! Please:

1. Fork the repo
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Write tests for new functionality
4. Run `make test` and `go vet ./...`
5. Commit with clear, descriptive messages
6. Open a pull request

Code style: idiomatic Go, proper error handling, clean interfaces. Each feature in its own package.
