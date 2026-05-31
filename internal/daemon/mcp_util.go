package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExtractMCPText pulls the text content from an MCP CallToolResult.
// Shared by extension watchers that communicate via MCP clients.
func ExtractMCPText(result any) string {
	type contentItem struct {
		Text string `json:"text"`
	}
	type callResult struct {
		Content []contentItem `json:"content"`
	}

	if r, ok := result.(interface{ GetContent() string }); ok {
		return r.GetContent()
	}

	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprint(result)
	}
	var cr callResult
	if err := json.Unmarshal(b, &cr); err == nil && len(cr.Content) > 0 {
		var parts []string
		for _, c := range cr.Content {
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(b)
}

// Truncate truncates a string to max characters.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
