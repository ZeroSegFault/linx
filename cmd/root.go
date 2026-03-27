package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/ZeroSegFault/linx/config"
	"github.com/ZeroSegFault/linx/tui"
	"github.com/spf13/cobra"
)

var (
	version    = "2.0.1"
	cfgFile    string
	showConfig bool

	// Management flags
	flagStatus   bool
	flagLogin    bool
	flagLogout   bool
	flagProvider string
	flagModels   bool
	flagDoctor   bool
	flagVersion  bool
	flagRollback bool
	flagLast     int
	flagMemory   bool
	flagEdit     bool
	flagClear    bool
	flagModel    string
	flagProfile  string
	flagProfiles bool
	flagHistory  bool
	flagSessions bool
	flagResume   string
	flagYes      bool
)

var rootCmd = &cobra.Command{
	Use:   "lx [prompt...]",
	Short: "Linx — AI-powered Linux system assistant",
	Long: `Linx (lx) configures and troubleshoots Linux systems using an LLM backend.

Run with no arguments for interactive TUI mode, or pass a prompt for single-shot CLI mode.
All management operations are flags — bare words are treated as a prompt.

Examples:
  lx                              Start interactive TUI
  lx fix my wifi                  CLI prompt (bare words, no quotes needed)
  lx "configure my display"       CLI prompt (quotes work too)
  lx -y "install nginx"           Auto-confirm destructive operations
  lx --status                     Auth status for all providers
  lx --login --provider codex     Authenticate with Codex (opens browser)
  lx --logout --provider codex    Clear Codex credentials
  lx --models                     List available models
  lx --doctor                     Health check
  lx --version                    Version info
  lx --rollback                   List and restore file backups
  lx --rollback --last 5          Show last 5 backups
  lx --memory                     Show persistent memory
  lx --model gpt-5.4 "fix my wifi" Use a specific model
  lx --profiles                   List configured provider profiles
  lx --profile cloud "fix wifi"   Use a specific profile
  lx --history                    Show recent session history
  lx --history --last 7           Show last 7 days of history
  lx --memory --edit              Open memory in $EDITOR
  lx --memory --clear             Clear memory with confirmation
  lx --sessions                   List recent sessions
  lx --sessions --last 20         Show last 20 sessions
  lx --sessions --clear           Clear archived sessions
  lx --resume a1b2                Resume session by UUID prefix
  lx --resume 1                   Resume session by list number`,
	Version:           version,
	Args:              cobra.ArbitraryArgs,
	RunE:              runRoot,
	DisableAutoGenTag: true,
	SilenceUsage:      true,
}

func init() {
	rootCmd.Flags().StringVar(&cfgFile, "config", "", "config file (default ~/.config/linx/config.toml)")
	rootCmd.Flags().BoolVar(&showConfig, "show-config", false, "print current config and exit")

	// Management flags
	rootCmd.Flags().BoolVar(&flagStatus, "status", false, "show auth status for all providers")
	rootCmd.Flags().BoolVar(&flagLogin, "login", false, "authenticate with an LLM provider (requires --provider)")
	rootCmd.Flags().BoolVar(&flagLogout, "logout", false, "clear credentials for an LLM provider (requires --provider)")
	rootCmd.Flags().StringVar(&flagProvider, "provider", "", "provider name (used with --login/--logout)")
	rootCmd.Flags().BoolVar(&flagModels, "models", false, "list available models from the configured provider")
	rootCmd.Flags().BoolVar(&flagDoctor, "doctor", false, "run health check")
	rootCmd.Flags().BoolVar(&flagVersion, "version", false, "print version and build info")
	rootCmd.Flags().BoolVar(&flagRollback, "rollback", false, "list and restore file backups")
	rootCmd.Flags().IntVar(&flagLast, "last", 0, "show last N backup entries (used with --rollback)")
	rootCmd.Flags().BoolVar(&flagMemory, "memory", false, "show persistent memory (combine with --edit or --clear)")
	rootCmd.Flags().BoolVar(&flagEdit, "edit", false, "open memory in $EDITOR (used with --memory)")
	rootCmd.Flags().BoolVar(&flagClear, "clear", false, "wipe all persistent memory (used with --memory)")
	rootCmd.Flags().StringVar(&flagModel, "model", "", "override model for this invocation")
	rootCmd.Flags().StringVar(&flagProfile, "profile", "", "use a named provider profile")
	rootCmd.Flags().BoolVar(&flagProfiles, "profiles", false, "list available provider profiles")
	rootCmd.Flags().BoolVar(&flagHistory, "history", false, "show recent session history")
	rootCmd.Flags().BoolVar(&flagSessions, "sessions", false, "list recent sessions")
	rootCmd.Flags().StringVar(&flagResume, "resume", "", "resume a previous session by UUID prefix or number")
	rootCmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "auto-confirm all destructive operations")
}

func Execute() error {
	return rootCmd.Execute()
}

// resolveProvider resolves the active provider profile and applies model override.
func resolveProvider(cfg *config.Config) error {
	providerCfg, err := cfg.ResolveProvider(flagProfile)
	if err != nil {
		return err
	}
	cfg.Provider = *providerCfg
	if flagModel != "" {
		cfg.Provider.Model = flagModel
	}
	return nil
}

func runRoot(cmd *cobra.Command, args []string) error {
	// Check flags in priority order

	if flagVersion {
		return runVersion()
	}

	if flagDoctor {
		return runDoctor()
	}

	if flagProfiles {
		return runProfiles()
	}

	if flagStatus {
		return runStatus()
	}

	if flagLogin {
		if flagProvider == "" {
			return fmt.Errorf("--provider is required with --login (e.g. lx --login --provider codex)")
		}
		return runLogin(flagProvider)
	}

	if flagLogout {
		if flagProvider == "" {
			return fmt.Errorf("--provider is required with --logout (e.g. lx --logout --provider codex)")
		}
		return runLogout(flagProvider)
	}

	if flagModels {
		return runModels()
	}

	if flagRollback {
		return runRollback(flagLast)
	}

	if flagMemory {
		if flagEdit {
			return runMemory("edit")
		}
		if flagClear {
			return runMemory("clear")
		}
		return runMemory("list")
	}

	if flagHistory {
		return runHistory()
	}

	if flagSessions {
		if flagClear {
			return runSessionsClear()
		}
		return runSessions()
	}

	if flagResume != "" {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if err := resolveProvider(cfg); err != nil {
			return err
		}
		return runResume(cfg, flagResume)
	}

	// Load config and resolve provider for show-config, CLI, and TUI paths
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := resolveProvider(cfg); err != nil {
		return err
	}

	// Apply --yes flag
	if flagYes {
		cfg.Behavior.ConfirmDestructive = false
	}

	if showConfig {
		// Active profile: resolved above
		activeProfile := flagProfile
		if activeProfile == "" {
			activeProfile = cfg.DefaultProfile
		}
		if activeProfile == "" {
			activeProfile = "(default)"
		}
		fmt.Printf("# Active profile: %s\n", activeProfile)
		return config.Print(cfg, os.Stdout)
	}

	// Bare words → CLI prompt
	if len(args) > 0 {
		prompt := strings.Join(args, " ")
		return runCLI(cfg, prompt)
	}

	// No args, no flags → TUI mode (if TTY)
	if !isTerminal() {
		return cmd.Help()
	}
	return tui.Run(cfg)
}

// --- Version ---

func runVersion() error {
	fmt.Printf("Linx (lx) v%s\n", version)
	fmt.Printf("Go:   %s\n", runtime.Version())
	fmt.Printf("OS:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}
