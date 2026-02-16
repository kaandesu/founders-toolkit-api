package scanmanager

import (
	"founders-toolkit-api/internal/database"
	"founders-toolkit-api/internal/response"
	"founders-toolkit-api/models"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// ---- Core types ----

type QueryType string

const (
	QueryTypeDirect       QueryType = "direct"
	QueryTypeIntermediate QueryType = "intermediate"
	QueryTypeIndirect     QueryType = "indirect"
)

type SiteInput struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Language    string `json:"language"`
}

type BrandCitation struct {
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Citations []string `json:"citations"`
}

type QueryBrandsResult struct {
	Query  string          `json:"query"`
	Brands []BrandCitation `json:"brands"`
}

type QueryGroup struct {
	Queries []QueryBrandsResult `json:"queries"`
}

type FinalBrandAnalysis struct {
	Indirect     QueryGroup `json:"indirect"`
	Intermediate QueryGroup `json:"intermediate"`
	Direct       QueryGroup `json:"direct"`
}

// Low-level helper: call Responses API and return concatenated text output.
func callOpenAIText(
	ctx context.Context,
	client *openai.Client,
	model shared.ChatModel,
	input string,
	tools []responses.ToolUnionParam,
	toolChoice *responses.ResponseNewParamsToolChoiceUnion,
) (string, error) {
	// Short preview of the prompt for the logs
	snippet := input
	if len(snippet) > 120 {
		snippet = snippet[:120] + "..."
	}

	log.Printf("[OpenAI] callOpenAIText: model=%s tools=%d snippet=%q", model, len(tools), snippet)

	params := responses.ResponseNewParams{
		Model: model,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: param.Opt[string]{Value: input},
		},
	}

	if len(tools) > 0 {
		params.Tools = tools
	}
	if toolChoice != nil {
		params.ToolChoice = *toolChoice
	}

	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		log.Printf("[OpenAI] ERROR: %v", err)
		return "", err
	}

	out := resp.OutputText()
	out = strings.TrimSpace(out)
	log.Printf("[OpenAI] callOpenAIText: got output len=%d", len(out))

	if out == "" {
		return "", errors.New("empty output from OpenAI")
	}
	return out, nil
}

// Generate N queries of a specific type for a given site.
func GenerateQueriesForType(
	ctx context.Context,
	client *openai.Client,
	site SiteInput,
	qType QueryType,
	n int,
) ([]string, error) {
	log.Printf("[GenerateQueriesForType] START type=%s n=%d site=%s", qType, n, site.URL)

	if n <= 0 {
		log.Printf("[GenerateQueriesForType] n <= 0, returning empty slice")
		return []string{}, nil
	}

	systemInstructions := fmt.Sprintf(`
You are an SEO query generator.

Given the following site, generate EXACTLY %d distinct %s queries
in the site's language (%s).

Rules:
- Return ONLY a JSON array of strings, e.g. ["query 1", "query 2", ...].
- No extra text, explanations, or comments.
- Queries must be 3-12 words.
- Do not include duplicate queries.
- For type "direct": must contain the brand or domain or clear brand token.
- For "intermediate": task/topic queries related to the product, NO brand tokens.
- For "indirect": broader, upstream intent queries, NO brand tokens.
`, n, qType, site.Language)

	prompt := systemInstructions + "\n\n" + buildSiteContext(site)

	text, err := callOpenAIText(
		ctx,
		client,
		shared.ChatModelGPT4_1Mini,
		prompt,
		nil,
		nil,
	)
	if err != nil {
		log.Printf("[GenerateQueriesForType] ERROR calling OpenAI: %v", err)
		return nil, err
	}

	jsonPart := extractJSONFromText(text)
	log.Printf("[GenerateQueriesForType] raw text len=%d jsonPart len=%d", len(text), len(jsonPart))

	var queries []string
	if err := json.Unmarshal([]byte(jsonPart), &queries); err != nil {
		log.Printf("[GenerateQueriesForType] ERROR parsing JSON: %v | raw=%s", err, text)
		return nil, fmt.Errorf("failed to parse queries JSON: %w (raw=%s)", err, text)
	}

	if len(queries) > n {
		queries = queries[:n]
	}
	log.Printf("[GenerateQueriesForType] DONE type=%s got %d queries: %+v", qType, len(queries), queries)
	return queries, nil
}

// Thin wrappers for each type
func GenerateDirectQueries(ctx context.Context, client *openai.Client, site SiteInput, n int) ([]string, error) {
	return GenerateQueriesForType(ctx, client, site, QueryTypeDirect, n)
}

func GenerateIntermediateQueries(ctx context.Context, client *openai.Client, site SiteInput, n int) ([]string, error) {
	return GenerateQueriesForType(ctx, client, site, QueryTypeIntermediate, n)
}

func GenerateIndirectQueries(ctx context.Context, client *openai.Client, site SiteInput, n int) ([]string, error) {
	return GenerateQueriesForType(ctx, client, site, QueryTypeIndirect, n)
}

// For a single query: use web_search to gather research notes (free-form text).

// For a single query: use web_search to gather research notes (free-form text).
func RunWebSearchForQuery(
	ctx context.Context,
	client *openai.Client,
	query string,
	site SiteInput,
) (string, error) {
	log.Printf("[RunWebSearchForQuery] START query=%q site=%s", query, site.URL)

	instructions := fmt.Sprintf(`
You are a research assistant.

Use the web_search tool to research the query:

"%s"

Focus on brands and services that appear relevant to this query.
Return a concise English summary (or in the site's language) that lists:
- brand names
- their URLs if possible
- where you found them (domains / pages)

You may structure your answer as bullet points, but do NOT output JSON in this step.
`, query)

	tools := []responses.ToolUnionParam{
		{
			OfWebSearch: &responses.WebSearchToolParam{
				Type: responses.WebSearchToolTypeWebSearch,
			},
		},
	}
	toolChoice := responses.ResponseNewParamsToolChoiceUnion{
		OfToolChoiceMode: param.Opt[responses.ToolChoiceOptions]{
			Value: responses.ToolChoiceOptionsAuto,
		},
	}

	text, err := callOpenAIText(
		ctx,
		client,
		shared.ChatModelGPT4_1Mini,
		instructions,
		tools,
		&toolChoice,
	)
	if err != nil {
		log.Printf("[RunWebSearchForQuery] ERROR: %v", err)
		return "", err
	}

	log.Printf("[RunWebSearchForQuery] DONE query=%q researchTextLen=%d", query, len(text))
	return text, nil
}

// Given research text for a query, ask the model to output strict JSON with brands + citations.

// Given research text for a query, ask the model to output strict JSON with brands + citations.
func ExtractBrandsFromResearchText(
	ctx context.Context,
	client *openai.Client,
	query string,
	researchText string,
) ([]BrandCitation, error) {
	log.Printf("[ExtractBrandsFromResearchText] START query=%q researchTextLen=%d", query, len(researchText))

	prompt := fmt.Sprintf(`
You will receive some research notes that summarize web search results for this query:

"%s"

The notes may include brand names, their URLs, and the websites where they were mentioned.

Your job:
- Identify brands that appear.
- For each brand, output:
  - name: the brand name (string)
  - url: the brand's main URL if visible (string, can be empty if unknown)
  - citations: list of domains or full URLs where the brand was mentioned (array of strings)

OUTPUT FORMAT (STRICT):
{
  "brands": [
    {
      "name": "...",
      "url": "...",
      "citations": ["...", "..."]
    }
  ]
}

RULES:
- Output ONLY valid JSON as above. No extra text, no markdown.
- citations array must not be null; use [] if nothing is known.
- If you find no brands, return {"brands": []}.

Research notes:
----------------
%s
`, query, researchText)

	text, err := callOpenAIText(
		ctx,
		client,
		shared.ChatModelGPT4_1Mini,
		prompt,
		nil,
		nil,
	)
	if err != nil {
		log.Printf("[ExtractBrandsFromResearchText] ERROR callOpenAIText: %v", err)
		return nil, err
	}

	fmt.Println("TEXT HERE IS", text)

	jsonPart := extractJSONFromText(text)
	log.Printf("[ExtractBrandsFromResearchText] rawTextLen=%d jsonPartLen=%d, \n json=%s", len(text), len(jsonPart), jsonPart)

	var parsed struct {
		Brands []BrandCitation `json:"brands"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &parsed); err != nil {
		log.Printf("[ExtractBrandsFromResearchText] ERROR parsing JSON: %v | raw=%s", err, text)
		return nil, fmt.Errorf("failed to parse brands JSON: %w (raw=%s)", err, text)
	}

	for i := range parsed.Brands {
		if parsed.Brands[i].Citations == nil {
			parsed.Brands[i].Citations = []string{}
		}
	}

	log.Printf("[ExtractBrandsFromResearchText] DONE query=%q brandsCount=%d", query, len(parsed.Brands))
	fmt.Printf("BRAND is\n: %s", parsed.Brands)
	return parsed.Brands, nil
}

// Full pipeline for one query: web_search → extract brands.
func ProcessSingleQuery(
	ctx context.Context,
	client *openai.Client,
	query string,
	site SiteInput,
) (QueryBrandsResult, error) {
	// You can set a per-query timeout if you want:
	perQueryCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	researchText, err := RunWebSearchForQuery(perQueryCtx, client, query, site)
	if err != nil {
		return QueryBrandsResult{}, err
	}

	brands, err := ExtractBrandsFromResearchText(perQueryCtx, client, query, researchText)
	if err != nil {
		return QueryBrandsResult{}, err
	}

	return QueryBrandsResult{
		Query:  query,
		Brands: brands,
	}, nil
}

// Process all queries of one type (direct/intermediate/indirect)
func ProcessQueriesForType(
	ctx context.Context,
	client *openai.Client,
	site SiteInput,
	qType QueryType,
	queries []string,
) ([]QueryBrandsResult, error) {
	results := make([]QueryBrandsResult, 0, len(queries))
	for _, q := range queries {
		if strings.TrimSpace(q) == "" {
			continue
		}
		r, err := ProcessSingleQuery(ctx, client, q, site)
		if err != nil {
			return nil, fmt.Errorf("processing %s query %q failed: %w", qType, q, err)
		}
		results = append(results, r)
	}
	return results, nil
}

// This is the "do everything" function you can call from a handler or a background job.
type BrandWorkflowConfig struct {
	NumDirect       int
	NumIntermediate int
	NumIndirect     int
}

func RunFullBrandWorkflow(
	ctx context.Context,
	client *openai.Client,
	site SiteInput,
	cfg BrandWorkflowConfig,
) (FinalBrandAnalysis, error) {
	// 1) Generate queries for each type
	directQueries, err := GenerateDirectQueries(ctx, client, site, cfg.NumDirect)
	if err != nil {
		return FinalBrandAnalysis{}, fmt.Errorf("generate direct queries: %w", err)
	}

	fmt.Printf(">>> %+v", directQueries)
	intermediateQueries, err := GenerateIntermediateQueries(ctx, client, site, cfg.NumIntermediate)
	if err != nil {
		return FinalBrandAnalysis{}, fmt.Errorf("generate intermediate queries: %w", err)
	}
	indirectQueries, err := GenerateIndirectQueries(ctx, client, site, cfg.NumIndirect)
	if err != nil {
		return FinalBrandAnalysis{}, fmt.Errorf("generate indirect queries: %w", err)
	}
	fmt.Printf(">>> %+v", indirectQueries)

	// 2) For each type, run search + brand extraction per query
	directResults, err := ProcessQueriesForType(ctx, client, site, QueryTypeDirect, directQueries)
	if err != nil {
		return FinalBrandAnalysis{}, err
	}
	intermediateResults, err := ProcessQueriesForType(ctx, client, site, QueryTypeIntermediate, intermediateQueries)
	if err != nil {
		return FinalBrandAnalysis{}, err
	}
	indirectResults, err := ProcessQueriesForType(ctx, client, site, QueryTypeIndirect, indirectQueries)
	if err != nil {
		return FinalBrandAnalysis{}, err
	}

	fmt.Println("RESULTS--------------------------")
	fmt.Printf("%+v\n\n", directResults)
	fmt.Printf("%+v\n\n", intermediateResults)
	fmt.Printf("%+v\n\n", indirectResults)

	// 3) Assemble final JSON
	final := FinalBrandAnalysis{
		Indirect: QueryGroup{
			Queries: indirectResults,
		},
		Intermediate: QueryGroup{
			Queries: intermediateResults,
		},
		Direct: QueryGroup{
			Queries: directResults,
		},
	}

	fmt.Printf("final is\n %s", final)

	return final, nil
}

// Example request DTO for this brand workflow endpoint.
type BrandWorkflowRequest struct {
	Name        string `json:"name"        binding:"required"`
	URL         string `json:"url"         binding:"required"`
	Description string `json:"description" binding:"required"`
	Language    string `json:"language"    binding:"required"`

	NumDirect       int `json:"num_direct"       `
	NumIntermediate int `json:"num_intermediate" `
	NumIndirect     int `json:"num_indirect"     `
}

func BrandWorkflowHandler(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- auth ---
		uRaw, ok := c.Get("user")
		if !ok {
			response.Respond(c, http.StatusUnauthorized, "unauthorized", nil)
			return
		}
		user, ok := uRaw.(models.User)
		if !ok || user.ID == 0 {
			response.Respond(c, http.StatusUnauthorized, "unauthorized", nil)
			return
		}

		// --- parse request ---
		var req BrandWorkflowRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Respond(c, http.StatusBadRequest, err.Error(), nil)
			return
		}

		if req.NumDirect <= 0 {
			req.NumDirect = 1
		}
		if req.NumIntermediate <= 0 {
			req.NumIntermediate = 1
		}
		if req.NumIndirect <= 0 {
			req.NumIndirect = 1
		}

		// --- find site by (user_id, url) ---
		var site models.Site
		if err := db.DB.Table("sites").
			Where("user_id = ? AND url = ?", user.ID, req.URL).
			First(&site).Error; err != nil || site.ID == 0 {
			response.Respond(c, http.StatusNotFound, "site not found", nil)
			return
		}

		siteInput := SiteInput{
			Name:        site.Name,
			URL:         site.URL,
			Description: site.Description,
			Language:    site.Lang,
		}
		cfg := BrandWorkflowConfig{
			NumDirect:       req.NumDirect,
			NumIntermediate: req.NumIntermediate,
			NumIndirect:     req.NumIndirect,
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
		defer cancel()

		client := openai.NewClient()
		log.Printf("[BrandWorkflowHandler] user=%d site_id=%d url=%s cfg=%+v",
			user.ID, site.ID, site.URL, cfg)

		// --- run main workflow ---
		analysis, err := RunFullBrandWorkflow(ctx, &client, siteInput, cfg)
		if err != nil {
			log.Printf("[BrandWorkflowHandler] RunFullBrandWorkflow error: %v", err)
			response.Respond(c, http.StatusBadGateway, "openai error: "+err.Error(), nil)
			return
		}

		// --- compute raw brand counts for each type ---
		directBrands := countBrandsInGroup(analysis.Direct)
		interBrands := countBrandsInGroup(analysis.Intermediate)
		indirectBrands := countBrandsInGroup(analysis.Indirect)

		// --- percentage-based scores (0–100) ---
		const maxBrandsPerQuery = 10.0 // tweak as you like

		maxDirect := maxBrandsPerQuery * float64(cfg.NumDirect)
		if maxDirect == 0 {
			maxDirect = 1
		}
		maxIntermediate := maxBrandsPerQuery * float64(cfg.NumIntermediate)
		if maxIntermediate == 0 {
			maxIntermediate = 1
		}
		maxIndirect := maxBrandsPerQuery * float64(cfg.NumIndirect)
		if maxIndirect == 0 {
			maxIndirect = 1
		}

		directScore := (float64(directBrands) / maxDirect) * 100.0
		intermediateScore := (float64(interBrands) / maxIntermediate) * 100.0
		indirectScore := (float64(indirectBrands) / maxIndirect) * 100.0

		// weighted visibility (still 0–100)
		visibilityScore := 0.5*directScore + 0.3*intermediateScore + 0.2*indirectScore

		// --- collect all queries used ---
		allQueries := collectAllQueries(analysis)

		// --- generate suggestions (second OpenAI call) ---
		suggestions, err := GenerateSuggestionsForSite(ctx, &client, siteInput, analysis)
		if err != nil {
			log.Printf("[BrandWorkflowHandler] GenerateSuggestionsForSite error: %v", err)
			response.Respond(c, http.StatusBadGateway, "suggestions error: "+err.Error(), nil)
			return
		}

		// --- marshal full FinalBrandAnalysis for storage ---
		analysisBytes, err := json.Marshal(analysis)
		if err != nil {
			log.Printf("[BrandWorkflowHandler] json.Marshal analysis error: %v", err)
			response.Respond(c, http.StatusInternalServerError, "marshal analysis failed: "+err.Error(), nil)
			return
		}

		// --- build and save BrandAnalysis row ---
		ba := models.BrandAnalysis{
			SiteID:            site.ID,
			UserID:            user.ID,
			DirectScore:       directScore,
			IntermediateScore: intermediateScore,
			IndirectScore:     indirectScore,
			VisibilityScore:   visibilityScore,
			Suggestions:       models.StringArray(suggestions),
			Queries:           models.StringArray(allQueries),
			Analysis:          models.JSONB(analysisBytes),
		}

		if err := db.DB.Table("brand_analyses").Create(&ba).Error; err != nil {
			log.Printf("[BrandWorkflowHandler] DB create error: %v", err)
			response.Respond(c, http.StatusInternalServerError, "brand analysis save failed: "+err.Error(), gin.H{
				"analysis":    analysis,
				"suggestions": suggestions,
				"queries":     allQueries,
			})
			return
		}

		log.Printf(
			"[BrandWorkflowHandler] saved brand_analyses id=%d site_id=%d user_id=%d scores={d=%.2f i=%.2f n=%.2f vis=%.2f} suggestions=%d queries=%d",
			ba.ID, ba.SiteID, ba.UserID,
			directScore, intermediateScore, indirectScore, visibilityScore,
			len(suggestions), len(allQueries),
		)

		// --- final response ---
		response.Respond(c, http.StatusOK, "ok", gin.H{
			"brand_analysis_id": ba.ID,
			"site_id":           site.ID,
			"scores": gin.H{
				"direct":       directScore,
				"intermediate": intermediateScore,
				"indirect":     indirectScore,
				"visibility":   visibilityScore,
			},
			"queries":     allQueries,
			"suggestions": suggestions,
			"analysis":    analysis,
		})
	}
}

func BrandDebugPing() gin.HandlerFunc {
	return func(c *gin.Context) {
		log.Printf("[BrandDebugPing] START")
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
		log.Printf("[BrandDebugPing] AFTER c.JSON")
	}
}

// count total brands across all queries in a group
func countBrandsInGroup(g QueryGroup) int {
	total := 0
	for _, q := range g.Queries {
		total += len(q.Brands)
	}
	return total
}

func collectAllQueries(analysis FinalBrandAnalysis) []string {
	m := make(map[string]struct{})

	add := func(qs []QueryBrandsResult) {
		for _, q := range qs {
			t := strings.TrimSpace(q.Query)
			if t == "" {
				continue
			}
			m[t] = struct{}{}
		}
	}

	add(analysis.Direct.Queries)
	add(analysis.Intermediate.Queries)
	add(analysis.Indirect.Queries)

	result := make([]string, 0, len(m))
	for q := range m {
		result = append(result, q)
	}
	// optional: sort.Strings(result)
	return result
}

func GenerateSuggestionsForSite(
	ctx context.Context,
	client *openai.Client,
	site SiteInput,
	analysis FinalBrandAnalysis,
) ([]string, error) {
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("marshal analysis for suggestions: %w", err)
	}

	prompt := fmt.Sprintf(`
You are an SEO strategist.

You will receive:
1) Target site:
   - name: %s
   - url: %s
   - description: %s
   - language/region: %s

2) A JSON object called FinalBrandAnalysis with:
   - direct queries (brand-aware)
   - intermediate queries (product / task / use-case)
   - indirect queries (broader upstream intent)
Each query contains multiple competing brands, their URLs, and the pages/domains where they were cited.

Your tasks:
- Compare the target site against all the competitors that appear in the analysis.
- Pay attention to:
  - where competitors are cited (domains/pages),
  - how often they appear across queries and query types,
  - what they seem to be doing that the target is not (content, landing pages, tools, comparison pages, etc.).
- Think in terms of realistic SEO / content / product suggestions that the target site could implement.

OUTPUT FORMAT (STRICT):
{
  "suggestions": [
    "One short, concrete suggestion...",
    "Another short, concrete suggestion..."
  ]
}

Rules:
- Maximum 10 suggestions.
- Each suggestion: 1–2 sentences, absolutely practical and specific to THIS target site.
- Do NOT mention JSON structure or internal details.
- Output ONLY valid JSON in the exact schema above. No markdown, no explanations.

FinalBrandAnalysis JSON:
------------------------
%s
`, site.Name, site.URL, site.Description, site.Language, string(analysisJSON))

	text, err := callOpenAIText(
		ctx,
		client,
		shared.ChatModelGPT4_1Mini,
		prompt,
		nil,
		nil,
	)
	if err != nil {
		return nil, err
	}

	jsonPart := extractJSONFromText(text)
	var parsed struct {
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &parsed); err != nil {
		return nil, fmt.Errorf("parse suggestions JSON failed: %w (raw=%s)", err, text)
	}

	// normalize
	out := make([]string, 0, len(parsed.Suggestions))
	for _, s := range parsed.Suggestions {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}

	return out, nil
}

func ListBrandAnalysesForSite(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- auth ---
		uRaw, _ := c.Get("user")
		user, _ := uRaw.(models.User)

		if user.ID == 0 {
			response.Respond(c, http.StatusUnauthorized, "unauthorized", nil)
			return
		}

		siteID := c.Param("id")

		// --- ensure site belongs to this user ---
		var site models.Site
		if err := db.DB.
			Where("id = ? AND user_id = ?", siteID, user.ID).
			First(&site).Error; err != nil || site.ID == 0 {
			response.Respond(c, http.StatusNotFound, "site not found", nil)
			return
		}

		// --- load brand_analyses rows for this site/user ---
		var analyses []models.BrandAnalysis
		if err := db.DB.
			Table("brand_analyses").
			Where("site_id = ? AND user_id = ?", site.ID, user.ID).
			Order("created_at DESC").
			Find(&analyses).Error; err != nil {

			response.Respond(c, http.StatusInternalServerError, "failed to load brand analyses", nil)
			return
		}

		response.Respond(c, http.StatusOK, "Brand analyses loaded", analyses)
	}
}
