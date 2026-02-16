package scanmanager

import (
	"founders-toolkit-api/internal/database"
	"founders-toolkit-api/internal/response"
	"founders-toolkit-api/models"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

/* ---------- request DTO ---------- */

type SEOScanRequest struct {
	Name        string `json:"name"        binding:"required"`
	URL         string `json:"url"         binding:"required"`
	Description string `json:"description" binding:"required"`
	Language    string `json:"language"    binding:"required"`
}

/* ---------- Final structured result ---------- */

type SEOAnalysisResult struct {
	Site struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		Description string `json:"description"`
		Language    string `json:"language"`
	} `json:"site"`
	Queries struct {
		Direct       []string `json:"direct"`
		Intermediate []string `json:"intermediate"`
		Indirect     []string `json:"indirect"`
	} `json:"queries"`
	PerQueryResults []struct {
		Type    string `json:"type"` // "direct" | "intermediate" | "indirect"
		Query   string `json:"query"`
		Results []struct {
			Rank          int     `json:"rank"` // 1..5
			Title         string  `json:"title"`
			URL           string  `json:"url"`
			Domain        string  `json:"domain"`
			Snippet       string  `json:"snippet"`
			IsMention     bool    `json:"is_mention"`
			MentionReason *string `json:"mention_reason"` // "domain" | "brand_in_text" | null
		} `json:"results"`
	} `json:"per_query_results"`
	Scores struct {
		DirectQueryScore              float64 `json:"direct_query_score"`
		IntermediateContextQueryScore float64 `json:"intermediate_context_query_score"`
		IndirectQueryScore            float64 `json:"indirect_query_score"`
		VisibilityScore               float64 `json:"visibility_score"`
	} `json:"scores"`
	Citations              []string `json:"citations"`
	KeywordsFromTheQueries []string `json:"keywords_from_the_queries"`
	AllOfTheQueriesUsed    []string `json:"all_of_the_queries_used"`
	Suggestions            []string `json:"suggestions"`
}

/* ---------- System prompt (STRICT, 1 query/type, tiny output) ---------- */

const systemPrompt = `
You are an AI SEO Analysis Agent.

INPUT (from user content):
	- site.url
- site.name
- site.description
- site.language

DERIVE brand_tokens (lowercased):
- full brand name (site.name)
- brand split into tokens
- registrable domain root from site.url (e.g., "github.com" → "github")
- variants with/without hyphens/spaces
Example: "Acme Tools" + "https://www.acme-tools.io" → {"acme","tools","acmetools","acme-tools","acme tools"}.

YOUR TASK (STRICT):
1) Generate EXACTLY 3 queries TOTAL:
   A) DIRECT: MUST contain ≥1 brand_token.
   B) INTERMEDIATE: relevant task/topic query, MUST NOT contain ANY brand_token.
      Examples of modifiers: pricing, features, integration, tutorial, documentation, status, roadmap, "how to …", "best … for …".
   C) INDIRECT: broad adjacent topic a prospect searches BEFORE knowing the brand, MUST NOT contain ANY brand_token; avoid brand-unique terms.
   - All queries must be in the given language and ≤ 12 words.
   - No duplicates.
   - Self-validate: if INTERMEDIATE or INDIRECT accidentally contain any brand_token, regenerate them; if DIRECT lacks a brand_token, regenerate it.

2) For EACH of the 3 queries you MUST call 'web_search' and keep ONLY the TOP 5 results.
   For each result record: rank (1..5), url, domain, title, snippet (≤ 180 chars).

3) MENTION LOGIC:
   is_mention = true if:
   - domain equals the target site's registrable domain, OR
   - title or snippet includes any brand_token (case-insensitive).
   mention_reason = "domain" | "brand_in_text" | null.

4) SCORING:
   weights: #1=1.0, #2=0.8, #3=0.6, #4=0.4, #5=0.2.
   For each type: score = (sum of weights for results with is_mention=true / number_of_queries_in_type) * 100.
   Overall visibility = 0.5*direct + 0.3*intermediate + 0.2*indirect.

5) LIMITS (to keep JSON small and stable):
   - keywords_from_the_queries: MAX 15 items, lowercase, deduped.
   - suggestions: MAX 8 items, each 1 sentence.
   - citations: MAX 10 unique items, prefer URLs; fallback to domains.

6) OUTPUT: SINGLE JSON OBJECT ONLY (no prose, no markdown fences):
{
  "site": { "name": "...", "url": "...", "description": "...", "language": "..." },
  "queries": {
    "direct": [ "<1 item>" ],
    "intermediate": [ "<1 item>" ],
    "indirect": [ "<1 item>" ]
  },
  "per_query_results": [
    {
      "type": "direct" | "intermediate" | "indirect",
      "query": "...",
      "results": [
        { "rank": 1..5, "title": "...", "url": "...", "domain": "...", "snippet": "...", "is_mention": true|false, "mention_reason": "domain"|"brand_in_text"|null }
      ]
    }
  ],
  "scores": {
    "direct_query_score": number,
    "intermediate_context_query_score": number,
    "indirect_query_score": number,
    "visibility_score": number
  },
  "citations": [strings],
  "keywords_from_the_queries": [strings],
  "all_of_the_queries_used": [strings], // exactly 3 in order: direct, intermediate, indirect
  "suggestions": [strings]
}
If 'web_search' is unavailable, return the schema with empty arrays and zeros (still valid JSON).
`

/* ---------- Responses API envelope (minimal) ---------- */

type responsesEnvelope struct {
	ID     string               `json:"id"`
	Status string               `json:"status"`
	Output []responsesOutputElt `json:"output"`
}

type responsesOutputElt struct {
	Type    string                    `json:"type"` // "message" or "web_search_call"
	Status  string                    `json:"status"`
	Action  *responsesWebSearchAction `json:"action,omitempty"`
	Content []responsesMessageContent `json:"content,omitempty"`
}

type responsesWebSearchAction struct {
	Type  string `json:"type"`  // "search"
	Query string `json:"query"` // issued query
}

type responsesMessageContent struct {
	Type        string        `json:"type"` // "output_text"
	Text        string        `json:"text"`
	Annotations []interface{} `json:"annotations"`
	Logprobs    []interface{} `json:"logprobs"`
}

/* ---------- Call Responses API with web_search tool ---------- */

func callResponsesWebSearch(ctx context.Context, sysPrompt, userContent string) (*SEOAnalysisResult, string, error) {
	payload := map[string]any{
		"model": "gpt-4o-mini",
		"input": []map[string]string{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": userContent},
		},
		"tools": []map[string]any{
			{"type": "web_search", "search_context_size": "low"},
		},
		"tool_choice": "auto",
		"temperature": 0.2,
		// Ask for plain text so we can parse the JSON string ourselves.
		"text": map[string]any{
			"format": map[string]any{"type": "text"},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	req.Header.Set("Content-Type", "application/json")
	if proj := os.Getenv("OPENAI_PROJECT_ID"); proj != "" {
		req.Header.Set("OpenAI-Project", proj)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)
	raw := string(respBody)

	if res.StatusCode >= 300 {
		return nil, raw, fmt.Errorf("openai error: %s | body=%s", res.Status, raw)
	}

	var env responsesEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, raw, fmt.Errorf("decode envelope error: %v | raw=%s", err, raw)
	}

	// Concatenate all output_text segments in message outputs
	var textBuf strings.Builder
	for _, out := range env.Output {
		if out.Type != "message" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && strings.TrimSpace(c.Text) != "" {
				textBuf.WriteString(c.Text)
			}
		}
	}
	jsonText := strings.TrimSpace(textBuf.String())
	if jsonText == "" {
		return nil, raw, errors.New("model returned empty message text")
	}

	// Remove code fences if present
	jsonText = stripCodeFences(jsonText)
	// Trim to balanced JSON (in case the tail got truncated)
	jsonText = trimToBalancedJSON(jsonText)

	var result SEOAnalysisResult
	if err := json.Unmarshal([]byte(jsonText), &result); err != nil {
		return nil, raw, fmt.Errorf("failed to parse model JSON: %v", err)
	}

	normalizeResult(&result)
	clampResult(&result) // enforce max lengths
	return &result, raw, nil
}

/* ---------- Helpers ---------- */

// strip ```json ... ``` fences if present
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// drop first line
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		} else {
			s = ""
		}
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

// trimToBalancedJSON finds the first '{' and the last matching '}' by brace count.
func trimToBalancedJSON(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return s
	}
	var depth int
	last := -1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				last = i
				break
			}
		}
	}
	if last >= start {
		return s[start : last+1]
	}
	// Fallback: return original
	return s
}

var rankWeights = map[int]float64{1: 1.0, 2: 0.8, 3: 0.6, 4: 0.4, 5: 0.2}

func normalizeResult(r *SEOAnalysisResult) {
	// Non-nil slices
	if r.Queries.Direct == nil {
		r.Queries.Direct = []string{}
	}
	if r.Queries.Intermediate == nil {
		r.Queries.Intermediate = []string{}
	}
	if r.Queries.Indirect == nil {
		r.Queries.Indirect = []string{}
	}
	if r.Citations == nil {
		r.Citations = []string{}
	}
	if r.KeywordsFromTheQueries == nil {
		r.KeywordsFromTheQueries = []string{}
	}
	if r.AllOfTheQueriesUsed == nil {
		r.AllOfTheQueriesUsed = []string{}
	}
	if r.Suggestions == nil {
		r.Suggestions = []string{}
	}

	// Ensure exactly 1 per type in queries (model should follow this, but enforce anyway)
	if len(r.Queries.Direct) > 1 {
		r.Queries.Direct = r.Queries.Direct[:1]
	}
	if len(r.Queries.Intermediate) > 1 {
		r.Queries.Intermediate = r.Queries.Intermediate[:1]
	}
	if len(r.Queries.Indirect) > 1 {
		r.Queries.Indirect = r.Queries.Indirect[:1]
	}

	// Derive all_of_the_queries_used if empty
	if len(r.AllOfTheQueriesUsed) == 0 {
		all := make([]string, 0, 3)
		all = append(all, r.Queries.Direct...)
		all = append(all, r.Queries.Intermediate...)
		all = append(all, r.Queries.Indirect...)
		r.AllOfTheQueriesUsed = all
	}

	// Recompute scores if all zeros (1 query per type)
	if len(r.PerQueryResults) > 0 &&
		r.Scores.DirectQueryScore == 0 &&
		r.Scores.IntermediateContextQueryScore == 0 &&
		r.Scores.IndirectQueryScore == 0 {

		var dSum, iSum, nSum float64
		var dCnt, iCnt, nCnt int

		for _, pq := range r.PerQueryResults {
			var weighted float64
			for _, re := range pq.Results {
				if re.IsMention {
					if w, ok := rankWeights[re.Rank]; ok {
						weighted += w
					}
				}
			}
			switch pq.Type {
			case "direct":
				dSum += weighted
				dCnt++
			case "intermediate":
				iSum += weighted
				iCnt++
			case "indirect":
				nSum += weighted
				nCnt++
			}
		}
		if dCnt == 0 {
			dCnt = 1
		}
		if iCnt == 0 {
			iCnt = 1
		}
		if nCnt == 0 {
			nCnt = 1
		}

		dScore := (dSum / float64(dCnt)) * 100.0
		iScore := (iSum / float64(iCnt)) * 100.0
		nScore := (nSum / float64(nCnt)) * 100.0
		vis := 0.5*dScore + 0.3*iScore + 0.2*nScore

		r.Scores.DirectQueryScore = dScore
		r.Scores.IntermediateContextQueryScore = iScore
		r.Scores.IndirectQueryScore = nScore
		r.Scores.VisibilityScore = vis
	}

	// Clean citations whitespace
	for i := range r.Citations {
		r.Citations[i] = strings.TrimSpace(r.Citations[i])
	}
}

func clampResult(r *SEOAnalysisResult) {
	// hard caps to avoid huge payloads and DB bloat
	if len(r.KeywordsFromTheQueries) > 15 {
		r.KeywordsFromTheQueries = r.KeywordsFromTheQueries[:15]
	}
	if len(r.Suggestions) > 8 {
		r.Suggestions = r.Suggestions[:8]
	}
	if len(r.Citations) > 10 {
		r.Citations = r.Citations[:10]
	}
	// Ensure exactly 3 queries in all_of_the_queries_used
	if len(r.AllOfTheQueriesUsed) > 3 {
		r.AllOfTheQueriesUsed = r.AllOfTheQueriesUsed[:3]
	}
}

/* ---------- Handler ---------- */

func AnalyzeAndCreateScan(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		uRaw, ok := c.Get("user")
		if !ok {
			response.Respond(c, http.StatusUnauthorized, "unauthorized", nil)
			return
		}
		user := uRaw.(models.User)
		if user.ID == 0 {
			response.Respond(c, http.StatusUnauthorized, "unauthorized", nil)
			return
		}

		var req SEOScanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Respond(c, http.StatusBadRequest, err.Error(), nil)
			return
		}

		// find the site by (user_id, url)
		var site models.Site
		if err := db.DB.Table("sites").
			Where("user_id = ? AND url = ?", user.ID, req.URL).
			First(&site).Error; err != nil || site.ID == 0 {
			response.Respond(c, http.StatusNotFound, "site not found", nil)
			return
		}

		// Build user content (fed to model as "user" message)
		userContent := "Site:\n" +
			"- Name: " + req.Name + "\n" +
			"- URL: " + req.URL + "\n" +
			"- Description: " + req.Description + "\n" +
			"- Language: " + req.Language + "\n\n" +
			"Perform the SEO visibility analysis per the system instructions."

		ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
		defer cancel()

		result, raw, err := callResponsesWebSearch(ctx, systemPrompt, userContent)
		if err != nil {
			response.Respond(c, http.StatusBadGateway, "openai error: "+err.Error(), gin.H{"raw": raw})
			return
		}

		// Persist Scan
		scan := models.Scan{
			SiteID:          site.ID,
			UserID:          user.ID,
			Completed:       true,
			Failed:          false,
			Score1:          result.Scores.DirectQueryScore,
			Score2:          result.Scores.IntermediateContextQueryScore,
			Score3:          result.Scores.IndirectQueryScore,
			VisibilityScore: result.Scores.VisibilityScore,
			Keywords:        models.StringArray(result.KeywordsFromTheQueries),
			Suggestions:     models.StringArray(result.Suggestions),
			Citations:       models.StringArray(result.Citations),
			Queries:         models.StringArray(result.AllOfTheQueriesUsed),
		}
		if err := db.DB.Create(&scan).Error; err != nil {
			response.Respond(c, http.StatusInternalServerError, "scan save failed: "+err.Error(), gin.H{
				"result": result,
				"raw":    raw,
			})
			return
		}

		response.Respond(c, http.StatusOK, "ok", gin.H{
			"scan_id": scan.ID,
			"result":  result,
		})
	}
}

/* ---------- (Optional) tiny util if you need domain from URL ---------- */
func domainFromURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	return strings.TrimPrefix(strings.TrimPrefix(host, "www."), "m.")
}
