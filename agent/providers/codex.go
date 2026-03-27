package providers

import (
	"fmt"
	"strings"

	"github.com/ZeroSegFault/linx/auth"
	"github.com/ZeroSegFault/linx/config"
)

// codexRelevantPrefixes defines model ID prefixes shown by `lx models` for the codex provider.
var codexRelevantPrefixes = []string{
	"codex-",
	"o4-",
	"o3-",
	"gpt-4o",
}

// Codex wraps the OpenAI provider with Codex-specific defaults.
// It uses the standard OpenAI API with OAuth access tokens from ChatGPT login.
type Codex struct {
	*OpenAI
}

// NewCodex creates a Codex provider backed by the OpenAI API using OAuth authentication.
// If no base URL is configured, it defaults to https://api.openai.com/v1.
// If no model is configured, it defaults to codex-mini-latest.
// Auth is handled via stored OAuth tokens (from `lx auth login`), not an API key.
func NewCodex(cfg *config.ProviderConfig) (*Codex, error) {
	oauthCfg := auth.DefaultOAuthConfig()

	// Get a valid access token (auto-refreshes if expired).
	accessToken, err := auth.GetValidToken(oauthCfg)
	if err != nil {
		return nil, fmt.Errorf("codex auth: %w\n  Run 'lx auth login' to authenticate with your ChatGPT account", err)
	}

	// Build a provider config with the OAuth token as the API key.
	resolved := *cfg
	if resolved.BaseURL == "" {
		resolved.BaseURL = "https://api.openai.com/v1"
	}
	if resolved.Model == "" {
		resolved.Model = "codex-mini-latest"
	}
	resolved.APIKey = accessToken

	inner, err := NewOpenAI(&resolved)
	if err != nil {
		return nil, err
	}

	return &Codex{OpenAI: inner}, nil
}

// ListModels returns models from the OpenAI API filtered to Codex-relevant ones.
func (c *Codex) ListModels() ([]ModelInfo, error) {
	all, err := c.OpenAI.ListModels()
	if err != nil {
		return nil, err
	}

	var filtered []ModelInfo
	for _, m := range all {
		if isCodexRelevant(m.ID) {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

// isCodexRelevant returns true if the model ID matches any Codex-relevant prefix.
func isCodexRelevant(id string) bool {
	lower := strings.ToLower(id)
	for _, prefix := range codexRelevantPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
