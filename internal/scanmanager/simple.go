package scanmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	// "github.com/openai/openai-go/v3"
	// "github.com/santhosh-tekuri/jsonschema"
)

type ScanRequest struct {
	Name        string `json:"name"        binding:"required"`
	URL         string `json:"url"         binding:"required"`
	Description string `json:"description" binding:"required"`
	Language    string `json:"language"    binding:"required"`
}

func CreateIndirectQuery() (string, *responses.Response) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := openai.NewClient()

	params := responses.ResponseNewParams{
		Model: shared.ChatModelGPT4_1Mini,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("Search current news about Tesla and summarize in 3 bullet points."),
		},

		Tools: []responses.ToolUnionParam{
			{
				OfWebSearch: &responses.WebSearchToolParam{
					Type: responses.WebSearchToolTypeWebSearch,
				},
			},
		},
		ToolChoice: responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.Opt[responses.ToolChoiceOptions]{
				Value: responses.ToolChoiceOptionsAuto,
			},
		},
	}

	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return "", nil
	}

	summary := resp.OutputText()

	return summary, resp
}

// TestSearch2 returns a handler that asks the Responses API to run a web search
// and summarize current news about Tesla in 3 bullets.
func TestSearch2() gin.HandlerFunc {
	return func(c *gin.Context) {
		// timeout for the whole request
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		client := openai.NewClient() // picks up OPENAI_API_KEY from env

		// Build the request params. We pass the user prompt as a simple string,
		// and we provide the web_search tool. ToolChoice "auto" lets the model
		// decide whether to call the web search tool.
		params := responses.ResponseNewParams{
			Model: shared.ChatModelGPT4_1Mini,
			Input: responses.ResponseNewParamsInputUnion{
				OfString: openai.String("Search current news about Tesla and summarize in 3 bullet points."),
			},
			// The Responses API supports specifying hosted tools such as web_search.
			// Use a ToolUnion with a ToolSpec pointing at type "web_search".
			Tools: []responses.ToolUnionParam{
				{
					OfWebSearch: &responses.WebSearchToolParam{
						Type: responses.WebSearchToolTypeWebSearch,
					},
				},
			},
			ToolChoice: responses.ResponseNewParamsToolChoiceUnion{
				OfToolChoiceMode: param.Opt[responses.ToolChoiceOptions]{
					Value: responses.ToolChoiceOptionsAuto,
				},
			},
		}

		resp, err := client.Responses.New(ctx, params)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}

		// Prefer returning a compact, useful result to the client.
		// OutputText() gives the text output concatenated from the response.
		summary := resp.OutputText()

		c.JSON(http.StatusOK, gin.H{
			"summary": summary,
			// include the raw response for debugging if you want
			"raw_response": resp,
		})
	}
}

func TestSearch() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// 🔍 Ask a simple question that forces a web search
		reqBody := map[string]any{
			"model": "gpt-4o-mini",
			"input": []map[string]any{
				{"role": "user", "content": "Search current news about Tesla and summarize in 3 bullet points."},
			},
			"tools": []map[string]any{
				{"type": "web_search"},
			},
			"tool_choice": "auto", // <-- let the model decide to call the search
		}

		buf, _ := json.Marshal(reqBody)
		req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/responses", bytes.NewBuffer(buf))
		req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)

		if resp.StatusCode >= 300 {
			c.JSON(http.StatusBadGateway, gin.H{
				"message": "OpenAI Error",
				"status":  resp.StatusCode,
				"body":    string(bodyBytes),
			})
			return
		}

		// return whatever OpenAI responded
		c.Data(resp.StatusCode, "application/json", bodyBytes)
	}
}

// A struct that will be converted to a Structured Outputs response schema
type HistoricalComputer struct {
	Origin       Origin   `json:"origin" jsonschema_description:"The origin of the computer"`
	Name         string   `json:"full_name" jsonschema_description:"The name of the device model"`
	Legacy       string   `json:"legacy" jsonschema:"enum=positive,enum=neutral,enum=negative" jsonschema_description:"Its influence on the field of computing"`
	NotableFacts []string `json:"notable_facts" jsonschema_description:"A few key facts about the computer"`
}

type Origin struct {
	YearBuilt    int64  `json:"year_of_construction" jsonschema_description:"The year it was made"`
	Organization string `json:"organization" jsonschema_description:"The organization that was in charge of its development"`
}

func GenerateSchema[T any]() interface{} {
	// Structured Outputs uses a subset of JSON schema
	// These flags are necessary to comply with the subset
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}

// Generate the JSON schema at initialization time
var HistoricalComputerResponseSchema = GenerateSchema[HistoricalComputer]()

func asdf() {
	client := openai.NewClient()
	ctx := context.Background()

	question := "What computer ran the first neural network?"

	print("> ")
	println(question)

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "historical_computer",
		Description: openai.String("Notable information about a computer"),
		Schema:      HistoricalComputerResponseSchema,
		Strict:      openai.Bool(true),
	}

	// Query the Chat Completions API
	chat, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(question),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		// Only certain models can perform structured outputs
		Model: openai.ChatModelGPT4o2024_08_06,
	})
	if err != nil {
		panic(err.Error())
	}

	// The model responds with a JSON string, so parse it into a struct
	var historicalComputer HistoricalComputer
	err = json.Unmarshal([]byte(chat.Choices[0].Message.Content), &historicalComputer)
	if err != nil {
		panic(err.Error())
	}

	// Use the model's structured response with a native Go struct
	fmt.Printf("Name: %v\n", historicalComputer.Name)
	fmt.Printf("Year: %v\n", historicalComputer.Origin.YearBuilt)
	fmt.Printf("Org: %v\n", historicalComputer.Origin.Organization)
	fmt.Printf("Legacy: %v\n", historicalComputer.Legacy)
	fmt.Printf("Facts:\n")
	for i, fact := range historicalComputer.NotableFacts {
		fmt.Printf("%v. %v\n", i+1, fact)
	}
}
