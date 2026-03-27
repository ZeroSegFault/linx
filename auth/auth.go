// Package auth handles OAuth 2.0 PKCE authentication with OpenAI for Codex.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// OpenAI OAuth endpoints (from well-known OpenID config).
	DefaultAuthURL  = "https://auth.openai.com/authorize"
	DefaultTokenURL = "https://auth0.openai.com/oauth/token"

	// Codex CLI public client ID.
	DefaultClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// OAuth scopes required for Codex API access.
	DefaultScopes = "openid profile email offline_access"

	// Callback path for the local HTTP server.
	CallbackPath = "/auth/callback"

	// File permissions for auth.json.
	authFileMode = 0o600
)

// OAuthConfig holds the OAuth endpoints and client configuration.
// Exported fields allow overriding for tests.
type OAuthConfig struct {
	AuthURL  string
	TokenURL string
	ClientID string
	Scopes   string
}

// DefaultOAuthConfig returns the production OpenAI OAuth configuration.
func DefaultOAuthConfig() *OAuthConfig {
	return &OAuthConfig{
		AuthURL:  DefaultAuthURL,
		TokenURL: DefaultTokenURL,
		ClientID: DefaultClientID,
		Scopes:   DefaultScopes,
	}
}

// TokenSet holds the OAuth tokens returned from the token endpoint.
type TokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope,omitempty"`
}

// IsExpired returns true if the access token has expired (with 60s buffer).
func (t *TokenSet) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false // no expiry info, assume valid
	}
	return time.Now().After(t.ExpiresAt.Add(-60 * time.Second))
}

// IsValid returns true if the token set has a non-empty access token.
func (t *TokenSet) IsValid() bool {
	return t.AccessToken != ""
}

// pkceChallenge holds PKCE code verifier and challenge.
type pkceChallenge struct {
	Verifier  string
	Challenge string
}

// generatePKCE creates a PKCE code verifier and S256 challenge.
func generatePKCE() (*pkceChallenge, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generating PKCE verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)

	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	return &pkceChallenge{Verifier: verifier, Challenge: challenge}, nil
}

// generateState creates a random OAuth state parameter.
func generateState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// authDir returns the directory for auth storage.
func authDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "linx")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "linx")
}

// AuthPath returns the path to auth.json.
func AuthPath() string {
	return filepath.Join(authDir(), "auth.json")
}

// SaveTokens writes the token set to disk with restricted permissions.
func SaveTokens(tokens *TokenSet) error {
	dir := authDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating auth dir: %w", err)
	}

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling tokens: %w", err)
	}

	return os.WriteFile(AuthPath(), data, authFileMode)
}

// LoadTokens reads the stored token set from disk.
func LoadTokens() (*TokenSet, error) {
	data, err := os.ReadFile(AuthPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // not logged in
		}
		return nil, fmt.Errorf("reading auth file: %w", err)
	}

	var tokens TokenSet
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parsing auth file: %w", err)
	}

	return &tokens, nil
}

// ClearTokens removes the stored auth file.
func ClearTokens() error {
	err := os.Remove(AuthPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing auth file: %w", err)
	}
	return nil
}

// RefreshAccessToken uses the refresh token to obtain a new access token.
func RefreshAccessToken(cfg *OAuthConfig, tokens *TokenSet) (*TokenSet, error) {
	if tokens.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available — run 'lx auth login' again")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {cfg.ClientID},
		"refresh_token": {tokens.RefreshToken},
	}

	return doTokenExchange(cfg.TokenURL, data)
}

// GetValidToken loads tokens, refreshes if expired, saves if refreshed, and returns a valid access token.
func GetValidToken(cfg *OAuthConfig) (string, error) {
	tokens, err := LoadTokens()
	if err != nil {
		return "", fmt.Errorf("loading auth: %w", err)
	}
	if tokens == nil || !tokens.IsValid() {
		return "", fmt.Errorf("not logged in — run 'lx auth login' first")
	}

	if !tokens.IsExpired() {
		return tokens.AccessToken, nil
	}

	// Token expired — try refresh.
	refreshed, err := RefreshAccessToken(cfg, tokens)
	if err != nil {
		return "", fmt.Errorf("token expired and refresh failed: %w — run 'lx auth login' again", err)
	}

	if err := SaveTokens(refreshed); err != nil {
		// Non-fatal: token is valid, just couldn't persist.
		fmt.Fprintf(os.Stderr, "warning: could not save refreshed token: %v\n", err)
	}

	return refreshed.AccessToken, nil
}

// LoginFlow runs the full OAuth PKCE login flow:
// 1. Generate PKCE challenge and state
// 2. Start local callback server
// 3. Build and return the authorization URL for the user's browser
// 4. Wait for the callback with the auth code
// 5. Exchange code for tokens
// 6. Save tokens to disk
//
// openBrowser is a callback that receives the authorization URL and should open
// the user's browser. It returns an error if the browser can't be opened.
func LoginFlow(cfg *OAuthConfig, openBrowser func(string) error) (*TokenSet, error) {
	pkce, err := generatePKCE()
	if err != nil {
		return nil, err
	}

	state, err := generateState()
	if err != nil {
		return nil, err
	}

	// Find a free port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d%s", port, CallbackPath)

	// Build authorization URL.
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {cfg.ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {cfg.Scopes},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	authURL := cfg.AuthURL + "?" + params.Encode()

	// Channel to receive the auth code from the callback handler.
	type callbackResult struct {
		code string
		err  error
	}
	resultCh := make(chan callbackResult, 1)

	// Set up callback handler.
	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if errParam := q.Get("error"); errParam != "" {
			desc := q.Get("error_description")
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<html><body><h2>Login failed</h2><p>%s: %s</p><p>You can close this tab.</p></body></html>", errParam, desc)
			resultCh <- callbackResult{err: fmt.Errorf("OAuth error: %s — %s", errParam, desc)}
			return
		}

		returnedState := q.Get("state")
		if returnedState != state {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body><h2>Login failed</h2><p>State mismatch.</p></body></html>")
			resultCh <- callbackResult{err: fmt.Errorf("OAuth state mismatch")}
			return
		}

		code := q.Get("code")
		if code == "" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body><h2>Login failed</h2><p>No authorization code received.</p></body></html>")
			resultCh <- callbackResult{err: fmt.Errorf("no authorization code in callback")}
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h2>✅ Logged in to Linx!</h2><p>You can close this tab and return to your terminal.</p></body></html>")
		resultCh <- callbackResult{code: code}
	})

	server := &http.Server{Handler: mux}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			resultCh <- callbackResult{err: fmt.Errorf("callback server error: %w", err)}
		}
	}()

	// Open the user's browser.
	if err := openBrowser(authURL); err != nil {
		server.Close()
		return nil, fmt.Errorf("opening browser: %w", err)
	}

	// Wait for callback (with timeout).
	var result callbackResult
	select {
	case result = <-resultCh:
	case <-time.After(5 * time.Minute):
		server.Close()
		wg.Wait()
		return nil, fmt.Errorf("login timed out after 5 minutes — try again")
	}

	// Shut down the callback server.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server.Shutdown(ctx)
	wg.Wait()

	if result.err != nil {
		return nil, result.err
	}

	// Exchange code for tokens.
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {cfg.ClientID},
		"code":          {result.code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {pkce.Verifier},
	}

	tokens, err := doTokenExchange(cfg.TokenURL, data)
	if err != nil {
		return nil, err
	}

	// Save tokens.
	if err := SaveTokens(tokens); err != nil {
		return nil, fmt.Errorf("saving tokens: %w", err)
	}

	return tokens, nil
}

// doTokenExchange performs the POST to the token endpoint and returns the parsed TokenSet.
func doTokenExchange(tokenURL string, data url.Values) (*TokenSet, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.PostForm(tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	if raw.Error != "" {
		return nil, fmt.Errorf("token exchange failed: %s — %s", raw.Error, raw.ErrorDesc)
	}

	if raw.AccessToken == "" {
		return nil, fmt.Errorf("token exchange returned empty access token (HTTP %d)", resp.StatusCode)
	}

	expiresAt := time.Time{}
	if raw.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second)
	}

	tokens := &TokenSet{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		IDToken:      raw.IDToken,
		TokenType:    raw.TokenType,
		ExpiresIn:    raw.ExpiresIn,
		ExpiresAt:    expiresAt,
		Scope:        raw.Scope,
	}

	// Preserve refresh token from previous tokens if not returned.
	if tokens.RefreshToken == "" {
		existing, _ := LoadTokens()
		if existing != nil && existing.RefreshToken != "" {
			tokens.RefreshToken = existing.RefreshToken
		}
	}

	return tokens, nil
}

// OpenBrowserFunc opens a URL in the user's default browser.
// Works on Linux using xdg-open.
func OpenBrowserFunc(url string) error {
	// Try xdg-open first (Linux standard), then sensible-browser, then open (macOS).
	browsers := []string{"xdg-open", "sensible-browser", "open"}
	for _, browser := range browsers {
		path, err := findExecutable(browser)
		if err != nil {
			continue
		}
		cmd := execCommand(path, url)
		if err := cmd.Start(); err != nil {
			continue
		}
		return nil
	}
	return fmt.Errorf("could not find a browser to open — visit the URL manually")
}

// findExecutable and execCommand are variables for testing.
var findExecutable = findExec
var execCommand = newExecCmd

func findExec(name string) (string, error) {
	// Use PATH lookup.
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found", name)
}

type execCmd struct {
	path string
	args []string
}

func newExecCmd(path string, args ...string) *execCmd {
	return &execCmd{path: path, args: args}
}

func (c *execCmd) Start() error {
	// Use os/exec for real execution.
	cmd := osexecCommand(c.path, c.args...)
	return cmd.Start()
}

// osexecCommand is the real os/exec.Command — declared as a var for mocking.
var osexecCommand = realExecCommand

func realExecCommand(name string, args ...string) interface{ Start() error } {
	cmd := &realCmd{name: name, args: args}
	return cmd
}

type realCmd struct {
	name string
	args []string
	cmd  *os.Process
}

func (c *realCmd) Start() error {
	// We only need to start, not wait.
	attr := &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}
	proc, err := os.StartProcess(c.name, append([]string{c.name}, c.args...), attr)
	if err != nil {
		return err
	}
	// Release immediately — we don't wait for the browser.
	proc.Release()
	return nil
}
