package scanmanager

import (
	"fmt"
	"strings"
)

// Build the site context as text for prompts.
func buildSiteContext(site SiteInput) string {
	return fmt.Sprintf(
		"Site:\n- Name: %s\n- URL: %s\n- Description: %s\n- Language: %s\n",
		site.Name,
		site.URL,
		site.Description,
		site.Language,
	)
}

// Extract JSON portion from model output (similar to your trimToBalancedJSON/strip fences).
func extractJSONFromText(s string) string {
	s = strings.TrimSpace(s)

	// Strip ``` fences if present
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		} else {
			s = ""
		}
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	s = strings.TrimSpace(s)

	start := strings.IndexByte(s, '{')
	if start < 0 {
		start = strings.IndexByte(s, '[')
	}
	if start < 0 {
		return s
	}

	var depth int
	last := -1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				last = i
				break
			}
		}
	}
	if last >= start {
		return strings.TrimSpace(s[start : last+1])
	}
	return s
}
