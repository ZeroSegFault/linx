package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZeroSegFault/linx/config"
)

func TestWebSearchNoAPIKey(t *testing.T) {
	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	args, _ := json.Marshal(map[string]string{"query": "test"})
	result, err := r.Execute("web_search", args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not configured") {
		t.Errorf("expected 'not configured' message, got: %s", result)
	}
}

func TestFetchURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Hello</h1><p>World</p></body></html>"))
	}))
	defer server.Close()

	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	args, _ := json.Marshal(map[string]string{"url": server.URL})
	result, err := r.Execute("fetch_url", args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Hello") {
		t.Errorf("expected 'Hello' in result, got: %s", result)
	}
	if !strings.Contains(result, "World") {
		t.Errorf("expected 'World' in result, got: %s", result)
	}
	// Should not contain HTML tags
	if strings.Contains(result, "<h1>") {
		t.Error("HTML tags should be stripped")
	}
}

func TestFetchURLEmpty(t *testing.T) {
	r := NewRegistry(nil, &config.ToolsConfig{}, false, true)
	args, _ := json.Marshal(map[string]string{"url": ""})
	result, err := r.Execute("fetch_url", args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "required") {
		t.Errorf("expected error message, got: %s", result)
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		input    string
		contains string
		excludes string
	}{
		{
			input:    "<p>Hello &amp; World</p>",
			contains: "Hello & World",
			excludes: "<p>",
		},
		{
			input:    "<script>alert('xss')</script><p>safe</p>",
			contains: "safe",
			excludes: "alert",
		},
		{
			input:    "<style>.x{color:red}</style><div>visible</div>",
			contains: "visible",
			excludes: "color",
		},
	}

	for _, tt := range tests {
		result := stripHTML(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("expected %q in result of stripHTML(%q), got %q", tt.contains, tt.input, result)
		}
		if tt.excludes != "" && strings.Contains(result, tt.excludes) {
			t.Errorf("did not expect %q in result of stripHTML(%q), got %q", tt.excludes, tt.input, result)
		}
	}
}
