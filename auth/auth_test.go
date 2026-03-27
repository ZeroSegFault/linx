package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenSet_IsExpired(t *testing.T) {
	tests := []struct {
		name    string
		token   TokenSet
		expired bool
	}{
		{
			name:    "zero expiry is not expired",
			token:   TokenSet{AccessToken: "tok"},
			expired: false,
		},
		{
			name:    "future expiry is not expired",
			token:   TokenSet{AccessToken: "tok", ExpiresAt: time.Now().Add(10 * time.Minute)},
			expired: false,
		},
		{
			name:    "past expiry is expired",
			token:   TokenSet{AccessToken: "tok", ExpiresAt: time.Now().Add(-10 * time.Minute)},
			expired: true,
		},
		{
			name:    "within 60s buffer is expired",
			token:   TokenSet{AccessToken: "tok", ExpiresAt: time.Now().Add(30 * time.Second)},
			expired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsExpired(); got != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

func TestTokenSet_IsValid(t *testing.T) {
	if (&TokenSet{}).IsValid() {
		t.Error("empty token should not be valid")
	}
	if !(&TokenSet{AccessToken: "test"}).IsValid() {
		t.Error("token with access_token should be valid")
	}
}

func TestSaveAndLoadTokens(t *testing.T) {
	// Use a temp dir for auth storage.
	tmpDir := t.TempDir()
	orig := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", orig)

	tokens := &TokenSet{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Truncate(time.Second),
	}

	if err := SaveTokens(tokens); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	// Check file permissions.
	info, err := os.Stat(filepath.Join(tmpDir, "linx", "auth.json"))
	if err != nil {
		t.Fatalf("stat auth.json: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected file mode 0600, got %o", perm)
	}

	// Load and verify.
	loaded, err := LoadTokens()
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if loaded.AccessToken != tokens.AccessToken {
		t.Errorf("access token mismatch: got %q, want %q", loaded.AccessToken, tokens.AccessToken)
	}
	if loaded.RefreshToken != tokens.RefreshToken {
		t.Errorf("refresh token mismatch: got %q, want %q", loaded.RefreshToken, tokens.RefreshToken)
	}
}

func TestLoadTokens_NotLoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", orig)

	tokens, err := LoadTokens()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokens != nil {
		t.Errorf("expected nil tokens when not logged in, got %+v", tokens)
	}
}

func TestClearTokens(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", orig)

	// Save then clear.
	SaveTokens(&TokenSet{AccessToken: "test"})

	if err := ClearTokens(); err != nil {
		t.Fatalf("ClearTokens: %v", err)
	}

	tokens, _ := LoadTokens()
	if tokens != nil {
		t.Error("tokens should be nil after clear")
	}

	// Clear again should not error.
	if err := ClearTokens(); err != nil {
		t.Fatalf("ClearTokens (already cleared): %v", err)
	}
}

func TestRefreshAccessToken(t *testing.T) {
	// Mock token endpoint.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		r.ParseForm()
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("expected grant_type=refresh_token, got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("refresh_token") != "refresh-abc" {
			t.Errorf("expected refresh_token=refresh-abc, got %s", r.FormValue("refresh_token"))
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer ts.Close()

	// Use temp dir to avoid loading real auth.json.
	tmpDir := t.TempDir()
	orig := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", orig)

	cfg := &OAuthConfig{
		TokenURL: ts.URL,
		ClientID: "test-client",
	}

	tokens := &TokenSet{
		AccessToken:  "old-token",
		RefreshToken: "refresh-abc",
	}

	refreshed, err := RefreshAccessToken(cfg, tokens)
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}

	if refreshed.AccessToken != "new-access-token" {
		t.Errorf("expected new-access-token, got %q", refreshed.AccessToken)
	}
	if refreshed.RefreshToken != "new-refresh-token" {
		t.Errorf("expected new-refresh-token, got %q", refreshed.RefreshToken)
	}
}

func TestRefreshAccessToken_NoRefreshToken(t *testing.T) {
	cfg := &OAuthConfig{TokenURL: "http://unused", ClientID: "test"}
	tokens := &TokenSet{AccessToken: "expired"}

	_, err := RefreshAccessToken(cfg, tokens)
	if err == nil {
		t.Fatal("expected error for missing refresh token")
	}
}

func TestGeneratePKCE(t *testing.T) {
	pkce, err := generatePKCE()
	if err != nil {
		t.Fatalf("generatePKCE: %v", err)
	}
	if pkce.Verifier == "" {
		t.Error("verifier should not be empty")
	}
	if pkce.Challenge == "" {
		t.Error("challenge should not be empty")
	}
	if pkce.Verifier == pkce.Challenge {
		t.Error("verifier and challenge should differ")
	}
}

func TestGenerateState(t *testing.T) {
	s1, err := generateState()
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	s2, _ := generateState()
	if s1 == s2 {
		t.Error("two generated states should differ")
	}
}

func TestGetValidToken_NotLoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", orig)

	_, err := GetValidToken(DefaultOAuthConfig())
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
}

func TestGetValidToken_ValidToken(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", orig)

	SaveTokens(&TokenSet{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	token, err := GetValidToken(DefaultOAuthConfig())
	if err != nil {
		t.Fatalf("GetValidToken: %v", err)
	}
	if token != "valid-token" {
		t.Errorf("expected valid-token, got %q", token)
	}
}

func TestGetValidToken_ExpiredWithRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", tmpDir)
	defer os.Setenv("XDG_DATA_HOME", orig)

	// Mock token endpoint for refresh.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "refreshed-token",
			"refresh_token": "new-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer ts.Close()

	SaveTokens(&TokenSet{
		AccessToken:  "expired-token",
		RefreshToken: "my-refresh",
		ExpiresAt:    time.Now().Add(-10 * time.Minute),
	})

	cfg := &OAuthConfig{
		TokenURL: ts.URL,
		ClientID: "test",
	}

	token, err := GetValidToken(cfg)
	if err != nil {
		t.Fatalf("GetValidToken: %v", err)
	}
	if token != "refreshed-token" {
		t.Errorf("expected refreshed-token, got %q", token)
	}

	// Verify saved tokens were updated.
	saved, _ := LoadTokens()
	if saved.AccessToken != "refreshed-token" {
		t.Errorf("saved token should be refreshed-token, got %q", saved.AccessToken)
	}
}
