package cmd

import (
	"fmt"

	"github.com/ZeroSegFault/linx/agent/providers"
	"github.com/ZeroSegFault/linx/config"
)

func runModels() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Resolve provider profile
	providerCfg, err := cfg.ResolveProvider(flagProfile)
	if err != nil {
		return err
	}
	cfg.Provider = *providerCfg

	p, err := providers.NewFromConfig(&cfg.Provider)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	lister, ok := p.(providers.ModelLister)
	if !ok {
		return fmt.Errorf("the %q provider does not support listing models", cfg.Provider.Type)
	}

	models, err := lister.ListModels()
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	if len(models) == 0 {
		fmt.Println("No models found.")
		return nil
	}

	fmt.Printf("Available models (%s at %s):\n\n", cfg.Provider.Type, cfg.Provider.BaseURL)
	for _, m := range models {
		if m.Size > 0 {
			fmt.Printf("  • %s  (%.1f GB)\n", m.ID, float64(m.Size)/(1024*1024*1024))
		} else {
			fmt.Printf("  • %s\n", m.ID)
		}
	}

	current := cfg.Provider.Model
	if current != "" {
		fmt.Printf("\nCurrently configured: %s\n", current)
	}

	return nil
}

func runProfiles() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	profiles := cfg.ListProfiles()
	if len(profiles) == 0 {
		fmt.Println("No profiles configured.")
		fmt.Println("Add [profiles.name] sections to your config file.")
		return nil
	}

	defaultProfile := cfg.DefaultProfile
	fmt.Println("Available profiles:")
	fmt.Println()
	for _, name := range profiles {
		p := cfg.Profiles[name]
		if p.Type == "" && name == "default" {
			p = cfg.Provider
		}
		marker := "  "
		if name == defaultProfile {
			marker = "→ "
		}
		fmt.Printf("%s%-12s %s at %s (model: %s)\n", marker, name, p.Type, p.BaseURL, p.Model)
	}
	if defaultProfile != "" {
		fmt.Printf("\nDefault: %s\n", defaultProfile)
	}
	fmt.Println("\nUse: lx --profile <name> \"your prompt\"")
	return nil
}
