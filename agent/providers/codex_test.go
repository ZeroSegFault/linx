package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ZeroSegFault/linx/auth"
	"github.com/ZeroSegFault/linx/config"
)

// setupCodexAuth creates a temp auth dir with a valid OAuth token for testing.
func setupCodexAuth(t *testing.T) func() {
	t.Helper()
	tmpDir := t.TempDir()
	orig := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)

	// Save a valid test token.
	auth.SaveTokens(&auth.TokenSet{
		AccessToken:  "test-oauth-token",
		RefreshToken: "test-refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	})

	return func() {
		os.Setenv("XDG_DATA_HOME", orig)
	}
}

func TestNewCodex_WithOAuth(t *testing.T) {
	cleanup := setupCodexAuth(t)
	defer cleanup()

	cfg := &config.ProviderConfig{
		Type: "codex",
		// No APIKey — Codex uses OAuth.
	}

	c, err := NewCodex(cfg)
	if err != nil {
		t.Fatalf("NewCodex failed: %v", err)
	}
	if c.model != "codex-mini-latest" {
		t.Errorf("expected default model codex-mini-latest, got %q", c.model)
	}
}

func TestNewCodex_CustomModel(t *testing.T) {
	cleanup := setupCodexAuth(t)
	defer cleanup()

	cfg := &config.ProviderConfig{
		Type:    "codex",
		BaseURL: "https://api.openai.com/v1",
		Model:   "o4-mini",
	}

	c, err := NewCodex(cfg)
	if err != nil {
		t.Fatalf("NewCodex failed: %v", err)
	}
	if c.model != "o4-mini" {
		t.Errorf("expected model o4-mini, got %q", c.model)
	}
}

func TestNewCodex_NoAuth(t *testing.T) {
	// Use empty temp dir — no auth tokens.
	tmpDir := t.TempDir()
	orig := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", orig)

	cfg := &config.ProviderConfig{
		Type: "codex",
	}

	_, err := NewCodex(cfg)
	if err == nil {
		t.Fatal("expected error when no OAuth token is available")
	}
}

func TestCodex_ListModelsFiltering(t *testing.T) {
	cleanup := setupCodexAuth(t)
	defer cleanup()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "gpt-4o"},
				{ID: "gpt-4o-mini"},
				{ID: "gpt-3.5-turbo"},
				{ID: "codex-mini-latest"},
				{ID: "o4-mini"},
				{ID: "o3-mini"},
				{ID: "dall-e-3"},
				{ID: "whisper-1"},
				{ID: "text-embedding-3-small"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := &config.ProviderConfig{
		Type:    "codex",
		BaseURL: ts.URL,
		Model:   "codex-mini-latest",
	}

	c, err := NewCodex(cfg)
	if err != nil {
		t.Fatalf("NewCodex failed: %v", err)
	}

	models, err := c.ListModels()
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	expected := map[string]bool{
		"gpt-4o":            true,
		"gpt-4o-mini":       true,
		"codex-mini-latest": true,
		"o4-mini":           true,
		"o3-mini":           true,
	}

	if len(models) != len(expected) {
		ids := make([]string, len(models))
		for i, m := range models {
			ids[i] = m.ID
		}
		t.Fatalf("expected %d models, got %d: %v", len(expected), len(models), ids)
	}

	for _, m := range models {
		if !expected[m.ID] {
			t.Errorf("unexpected model in filtered list: %s", m.ID)
		}
	}
}

func TestIsCodexRelevant(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"codex-mini-latest", true},
		{"codex-mini", true},
		{"o4-mini", true},
		{"o3-mini", true},
		{"gpt-4o", true},
		{"gpt-4o-mini", true},
		{"gpt-3.5-turbo", false},
		{"dall-e-3", false},
		{"whisper-1", false},
		{"text-embedding-3-small", false},
	}

	for _, tt := range tests {
		got := isCodexRelevant(tt.id)
		if got != tt.want {
			t.Errorf("isCodexRelevant(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}
