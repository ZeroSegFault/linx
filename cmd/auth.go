package cmd

import (
	"fmt"

	"github.com/ZeroSegFault/linx/auth"
)

func runStatus() error {
	fmt.Println("🔑 Authentication Status")
	fmt.Println("════════════════════════════════════")

	// Codex
	tokens, err := auth.LoadTokens()
	if err != nil {
		fmt.Printf("❌ Codex:  error reading credentials — %v\n", err)
	} else if tokens == nil || !tokens.IsValid() {
		fmt.Println("⬚  Codex:  not logged in")
		fmt.Println("           Run: lx --login --provider codex")
	} else if tokens.IsExpired() {
		if tokens.RefreshToken != "" {
			fmt.Println("⚠️  Codex:  token expired (will auto-refresh on next use)")
		} else {
			fmt.Println("❌ Codex:  token expired (no refresh token — run lx --login --provider codex)")
		}
		if !tokens.ExpiresAt.IsZero() {
			fmt.Printf("           Expires: %s\n", tokens.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("           Credentials: %s\n", auth.AuthPath())
	} else {
		fmt.Println("✅ Codex:  logged in")
		if !tokens.ExpiresAt.IsZero() {
			fmt.Printf("           Expires: %s\n", tokens.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("           Credentials: %s\n", auth.AuthPath())
	}

	// Future: Anthropic, etc.

	fmt.Println("════════════════════════════════════")
	return nil
}

func runLogin(provider string) error {
	switch provider {
	case "codex":
		fmt.Println("🔐 Logging in to OpenAI Codex via ChatGPT...")
		fmt.Println("   Opening your browser for authentication...")

		cfg := auth.DefaultOAuthConfig()
		tokens, err := auth.LoginFlow(cfg, auth.OpenBrowserFunc)
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		fmt.Println("✅ Logged in to Codex successfully!")
		if !tokens.ExpiresAt.IsZero() {
			fmt.Printf("   Token expires: %s\n", tokens.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("   Credentials saved to %s\n", auth.AuthPath())
		return nil
	default:
		return fmt.Errorf("unsupported provider %q — supported: codex", provider)
	}
}

func runLogout(provider string) error {
	switch provider {
	case "codex":
		tokens, _ := auth.LoadTokens()
		if tokens == nil {
			fmt.Println("ℹ️  Not logged in to Codex.")
			return nil
		}

		if err := auth.ClearTokens(); err != nil {
			return fmt.Errorf("logout failed: %w", err)
		}

		fmt.Println("✅ Logged out of Codex. Credentials removed.")
		return nil
	default:
		return fmt.Errorf("unsupported provider %q — supported: codex", provider)
	}
}
