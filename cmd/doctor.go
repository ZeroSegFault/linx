package cmd

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ZeroSegFault/linx/auth"
	"github.com/ZeroSegFault/linx/backup"
	"github.com/ZeroSegFault/linx/config"
	"github.com/ZeroSegFault/linx/memory"
)

func runDoctor() error {
	fmt.Println("🩺 Linx Health Check")
	fmt.Println("═══════════════════════════════════════")

	allGood := true

	// 1. Config file
	cfgPath := config.DefaultPath()
	if cfgFile != "" {
		cfgPath = cfgFile
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Printf("❌ Config:    %s — %v\n", cfgPath, err)
		allGood = false
	} else {
		info, _ := os.Stat(cfgPath)
		if info != nil {
			fmt.Printf("✅ Config:    %s (%.0f bytes)\n", cfgPath, float64(info.Size()))
		} else {
			fmt.Printf("✅ Config:    %s (defaults)\n", cfgPath)
		}

		// Print config validation warnings
		for _, w := range cfg.Warnings {
			fmt.Printf("⚠️  Config:    %s\n", w)
			allGood = false
		}
	}

	if cfg == nil {
		fmt.Println("\n⚠️  Cannot continue without valid config.")
		return nil
	}

	// Check secrets file
	secretsPath := config.SecretsPath()
	if info, err := os.Stat(secretsPath); err == nil {
		mode := info.Mode().Perm()
		if mode == 0o600 {
			fmt.Printf("✅ Secrets:   %s (%.0f bytes, permissions OK)\n", secretsPath, float64(info.Size()))
		} else {
			fmt.Printf("⚠️  Secrets:   %s (permissions %o — should be 600)\n", secretsPath, mode)
		}
	} else if os.IsNotExist(err) {
		fmt.Printf("ℹ️  Secrets:   %s (not created — API keys in config.toml)\n", secretsPath)
	} else {
		fmt.Printf("❌ Secrets:   %s — %v\n", secretsPath, err)
	}

	// Resolve provider profile for doctor check
	providerCfg, resolveErr := cfg.ResolveProvider(flagProfile)
	if resolveErr != nil {
		fmt.Printf("⚠️  Profile:   %v\n", resolveErr)
		allGood = false
	} else {
		cfg.Provider = *providerCfg
		// Show active profile info
		profiles := cfg.ListProfiles()
		if len(profiles) > 1 || (len(profiles) == 1 && profiles[0] != "default") {
			activeProfile := flagProfile
			if activeProfile == "" {
				activeProfile = cfg.DefaultProfile
			}
			if activeProfile == "" {
				activeProfile = "(legacy)"
			}
			fmt.Printf("ℹ️  Profile:   %s (available: %s)\n", activeProfile, strings.Join(profiles, ", "))
		}
	}

	// 2. Provider connectivity
	providerURL := cfg.Provider.BaseURL
	if providerURL == "" {
		providerURL = "https://api.openai.com/v1"
	}

	pingURL := providerURL
	switch cfg.Provider.Type {
	case "ollama":
		pingURL = providerURL
	case "codex":
		if providerURL == "" {
			providerURL = "https://api.openai.com/v1"
		}
		pingURL = providerURL + "/models"
	default:
		pingURL = providerURL + "/models"
	}

	// Codex: check OAuth auth status
	if cfg.Provider.Type == "codex" {
		tokens, err := auth.LoadTokens()
		if err != nil {
			fmt.Printf("⚠️  Codex auth: error reading credentials — %v\n", err)
			allGood = false
		} else if tokens == nil || !tokens.IsValid() {
			fmt.Println("⚠️  Codex auth: not logged in — run 'lx --login --provider codex'")
			allGood = false
		} else if tokens.IsExpired() {
			if tokens.RefreshToken != "" {
				fmt.Println("ℹ️  Codex auth: token expired (will auto-refresh on next use)")
			} else {
				fmt.Println("⚠️  Codex auth: token expired — run 'lx --login --provider codex'")
				allGood = false
			}
		} else {
			fmt.Printf("✅ Codex auth: logged in (expires %s)\n", tokens.ExpiresAt.Format("2006-01-02 15:04"))
		}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(pingURL)
	if err != nil {
		fmt.Printf("❌ Provider:  %s (%s) — unreachable: %v\n", cfg.Provider.Type, providerURL, err)
		allGood = false
	} else {
		resp.Body.Close()
		switch {
		case resp.StatusCode == 200:
			fmt.Printf("✅ Provider:  %s at %s (model: %s)\n", cfg.Provider.Type, providerURL, cfg.Provider.Model)
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			fmt.Printf("⚠️  Provider:  %s at %s — reachable but authentication failed\n", cfg.Provider.Type, providerURL)
			allGood = false
		default:
			fmt.Printf("⚠️  Provider:  %s at %s — reachable but returned HTTP %d\n", cfg.Provider.Type, providerURL, resp.StatusCode)
			allGood = false
		}
	}

	// 3. Memory file
	memPath := memory.MemoryPath()
	memInfo, err := os.Stat(memPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("ℹ️  Memory:    %s (not yet created — will be created after first session)\n", memPath)
		} else {
			fmt.Printf("❌ Memory:    %s — %v\n", memPath, err)
			allGood = false
		}
	} else {
		fmt.Printf("✅ Memory:    %s (%.1f KB, updated %s)\n",
			memPath,
			float64(memInfo.Size())/1024,
			memInfo.ModTime().Format("2006-01-02 15:04"),
		)
	}

	// 4. Backup directory
	backupDir := backup.BackupsDir()
	idxPath := backup.IndexPath()
	idx, err := backup.LoadIndex()
	if err != nil {
		fmt.Printf("❌ Backups:   %s — %v\n", backupDir, err)
		allGood = false
	} else {
		if _, err := os.Stat(idxPath); os.IsNotExist(err) {
			fmt.Printf("ℹ️  Backups:   %s (no backups yet)\n", backupDir)
		} else {
			fmt.Printf("✅ Backups:   %s (%d entries)\n", backupDir, len(idx.Entries))
		}
	}

	// 5. Web search
	if cfg.Tools.BraveAPIKey != "" {
		fmt.Println("✅ Web search: Brave API key configured")
	} else {
		fmt.Println("ℹ️  Web search: not configured (add brave_api_key to [tools] in config)")
	}

	fmt.Println("═══════════════════════════════════════")
	if allGood {
		fmt.Println("✅ All checks passed!")
	} else {
		fmt.Println("⚠️  Some issues found — see above.")
	}

	return nil
}
