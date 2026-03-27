package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ZeroSegFault/linx/agent/providers"
)

// RegisterWebTools adds web_search and fetch_url tools to the registry.
func (r *Registry) RegisterWebTools(braveAPIKey string) {
	r.register(r.webSearch(braveAPIKey))
	r.register(r.fetchURL())
}

func (r *Registry) webSearch(apiKey string) *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "web_search",
			Description: "Search the web using Brave Search. Returns titles, URLs, and descriptions of matching results. Useful for researching Linux issues, finding documentation, or looking up error messages.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query",
					},
					"count": map[string]interface{}{
						"type":        "integer",
						"description": "Number of results to return (default 5, max 10)",
					},
				},
				"required": []string{"query"},
			},
		},
		Execute: func(args json.RawMessage) (string, error) {
			if apiKey == "" {
				return "Web search is not configured. To enable it, add your Brave Search API key to ~/.config/linx/config.toml:\n\n[tools]\nbrave_api_key = \"your-key-here\"\n\nGet a free API key at https://brave.com/search/api/", nil
			}

			var params struct {
				Query string `json:"query"`
				Count int    `json:"count"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}

			if params.Count <= 0 {
				params.Count = 5
			}
			if params.Count > 10 {
				params.Count = 10
			}

			u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
				url.QueryEscape(params.Query), params.Count)

			req, err := http.NewRequest("GET", u, nil)
			if err != nil {
				return "", fmt.Errorf("creating request: %w", err)
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("X-Subscription-Token", apiKey)

			client := &http.Client{Timeout: 15 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Sprintf("Search request failed: %v", err), nil
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Sprintf("Error reading response: %v", err), nil
			}

			if resp.StatusCode != http.StatusOK {
				return fmt.Sprintf("Brave Search API error (HTTP %d): %s", resp.StatusCode, string(body)), nil
			}

			var result braveSearchResponse
			if err := json.Unmarshal(body, &result); err != nil {
				return fmt.Sprintf("Error parsing search results: %v", err), nil
			}

			if len(result.Web.Results) == 0 {
				return "No results found.", nil
			}

			var sb strings.Builder
			for i, r := range result.Web.Results {
				sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Description))
			}
			return sb.String(), nil
		},
	}
}

type braveSearchResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

func (r *Registry) fetchURL() *Tool {
	return &Tool{
		Definition: providers.ToolDefinition{
			Name:        "fetch_url",
			Description: "Fetch a URL and return its text content. HTML tags are stripped, returning readable text. Useful for reading documentation, man pages, GitHub issues, or error references.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to fetch",
					},
				},
				"required": []string{"url"},
			},
		},
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}

			if params.URL == "" {
				return "Error: URL is required", nil
			}

			client := &http.Client{Timeout: 15 * time.Second}
			resp, err := client.Get(params.URL)
			if err != nil {
				return fmt.Sprintf("Error fetching URL: %v", err), nil
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Sprintf("HTTP %d fetching %s", resp.StatusCode, params.URL), nil
			}

			// Scale raw body limit with maxFetchChars (5x to account for HTML overhead)
			rawLimit := int64(r.maxFetchChars) * 5
			if rawLimit < 100*1024 {
				rawLimit = 100 * 1024 // minimum 100KB
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, rawLimit))
			if err != nil {
				return fmt.Sprintf("Error reading response: %v", err), nil
			}

			text := stripHTML(string(body))

			// Collapse whitespace
			spaceRe := regexp.MustCompile(`\s+`)
			text = spaceRe.ReplaceAllString(text, " ")
			text = strings.TrimSpace(text)

			runes := []rune(text)
			if len(runes) > r.maxFetchChars {
				text = string(runes[:r.maxFetchChars]) + fmt.Sprintf("\n\n[truncated — content exceeds %d chars]", r.maxFetchChars)
			}

			return text, nil
		},
	}
}

// stripHTML removes HTML tags from a string.
func stripHTML(s string) string {
	// Remove script blocks
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	s = scriptRe.ReplaceAllString(s, "")

	// Remove style blocks
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	s = styleRe.ReplaceAllString(s, "")

	// Remove HTML tags
	tagRe := regexp.MustCompile(`<[^>]*>`)
	s = tagRe.ReplaceAllString(s, " ")

	// Decode common HTML entities
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&nbsp;", " ",
	)
	s = replacer.Replace(s)

	return s
}
