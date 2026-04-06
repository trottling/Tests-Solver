package openaiagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testsolverbot/internal/config"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
)

type Client struct {
	config *config.Config
	cli    openai.Client
}

func New(c *config.Config) *Client {
	opts := []option.RequestOption{option.WithAPIKey(c.OpenAI.APIKey)}
	if c.OpenAI.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.OpenAI.BaseURL))
	}
	return &Client{cli: openai.NewClient(opts...), config: c}
}

type ImageInput struct {
	Data     []byte
	MimeType string
}

type ExtractedTask struct {
	Number              string   `json:"number"`
	TaskText            string   `json:"task_text"`
	Options             []string `json:"options"`
	SourceImages        []int    `json:"source_images"`
	UnreadableFragments []string `json:"unreadable_fragments"`
}

type extractedPayload struct {
	Tasks []ExtractedTask `json:"tasks"`
}

type SolveTask struct {
	Number              string   `json:"number"`
	Status              string   `json:"status"`
	SelectedOptions     []string `json:"selected_options"`
	AnswerText          string   `json:"answer_text"`
	Explanation         string   `json:"explanation"`
	UnreadableFragments []string `json:"unreadable_fragments"`
}

type SolveResult struct {
	Tasks []SolveTask `json:"tasks"`
}

var latexMarkers = []*regexp.Regexp{
	regexp.MustCompile(`\\\(`),
	regexp.MustCompile(`\\\)`),
	regexp.MustCompile(`\\\[`),
	regexp.MustCompile(`\\\]`),
	regexp.MustCompile(`\$\$`),
	regexp.MustCompile(`\\frac`),
	regexp.MustCompile(`\\sin`),
	regexp.MustCompile(`\\cos`),
}

func (c *Client) SolveImages(ctx context.Context, images []ImageInput) (*SolveResult, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("нет изображений для решения")
	}

	maxTokens := c.outputTokensForPages(len(images))

	extractResp, extractJSON, extractData, err := c.extractTasks(ctx, images, maxTokens)
	if err != nil {
		return nil, err
	}

	solveJSON, solveData, err := c.solveWithExtract(ctx, extractResp.ID, images, extractJSON, maxTokens)
	if err != nil {
		return nil, err
	}

	if err = c.validateSolvePayload(solveData, extractData); err != nil {
		return nil, err
	}

	if strings.TrimSpace(solveJSON) == "" {
		return nil, fmt.Errorf("получен пустой JSON-ответ от модели")
	}

	return solveData, nil
}

func (c *Client) extractTasks(ctx context.Context, images []ImageInput, maxTokens int64) (*responses.Response, string, extractedPayload, error) {
	payload := c.newExtractRequest(images, maxTokens)
	resp, out, data, err := c.runExtract(ctx, payload)
	if err != nil {
		return nil, "", extractedPayload{}, err
	}

	if resp.Status == responses.ResponseStatusIncomplete && resp.IncompleteDetails.Reason == "max_output_tokens" {
		payload.MaxOutputTokens = openai.Int(maxTokens + 10000)
		resp, out, data, err = c.runExtract(ctx, payload)
		if err != nil {
			return nil, "", extractedPayload{}, err
		}
	}

	if resp.Status != responses.ResponseStatusCompleted {
		return nil, "", extractedPayload{}, fmt.Errorf("этап извлечения завершился со статусом %s", resp.Status)
	}

	return resp, out, data, nil
}

func (c *Client) solveWithExtract(ctx context.Context, previousResponseID string, images []ImageInput, extractJSON string, maxTokens int64) (string, *SolveResult, error) {
	payload := c.newSolveRequest(previousResponseID, images, extractJSON, maxTokens)
	resp, out, data, err := c.runSolve(ctx, payload)
	if err != nil {
		return "", nil, err
	}

	if resp.Status == responses.ResponseStatusIncomplete && resp.IncompleteDetails.Reason == "max_output_tokens" {
		payload.MaxOutputTokens = openai.Int(maxTokens + 10000)
		resp, out, data, err = c.runSolve(ctx, payload)
		if err != nil {
			return "", nil, err
		}
	}

	if resp.Status != responses.ResponseStatusCompleted {
		return "", nil, fmt.Errorf("этап решения завершился со статусом %s", resp.Status)
	}

	return out, data, nil
}

func (c *Client) runExtract(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, string, extractedPayload, error) {
	resp, err := c.cli.Responses.New(ctx, params)
	if err != nil {
		return nil, "", extractedPayload{}, err
	}
	out := strings.TrimSpace(resp.OutputText())
	var data extractedPayload
	if err = json.Unmarshal([]byte(out), &data); err != nil {
		return nil, out, extractedPayload{}, fmt.Errorf("не удалось распарсить JSON извлечения: %w", err)
	}
	return resp, out, data, nil
}

func (c *Client) runSolve(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, string, *SolveResult, error) {
	resp, err := c.cli.Responses.New(ctx, params)
	if err != nil {
		return nil, "", nil, err
	}
	out := strings.TrimSpace(resp.OutputText())
	data := new(SolveResult)
	if err = json.Unmarshal([]byte(out), data); err != nil {
		return nil, out, nil, fmt.Errorf("не удалось распарсить JSON решения: %w", err)
	}
	return resp, out, data, nil
}

func (c *Client) newExtractRequest(images []ImageInput, maxTokens int64) responses.ResponseNewParams {
	return responses.ResponseNewParams{
		Model:           c.config.OpenAI.Model,
		Reasoning:       shared.ReasoningParam{Effort: shared.ReasoningEffort(c.config.OpenAI.Reason)},
		MaxOutputTokens: openai.Int(maxTokens),
		Store:           openai.Bool(true),
		Instructions:    openai.String(DeveloperInstructions),
		Input:           responses.ResponseNewParamsInputUnion{OfInputItemList: c.buildInput(ExtractUserPrompt, images, "extract")},
		Text: responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
			Name:        "extracted_tasks",
			Description: openai.String("Структура извлечённых задач"),
			Strict:      openai.Bool(true),
			Schema:      extractSchema(),
		}}},
	}
}

func (c *Client) newSolveRequest(previousResponseID string, images []ImageInput, extractJSON string, maxTokens int64) responses.ResponseNewParams {
	userPrompt := SolveUserPrompt + "\n\nJSON извлечения заданий:\n" + extractJSON
	return responses.ResponseNewParams{
		Model:              c.config.OpenAI.Model,
		Reasoning:          shared.ReasoningParam{Effort: shared.ReasoningEffort(c.config.OpenAI.Reason)},
		MaxOutputTokens:    openai.Int(maxTokens),
		Store:              openai.Bool(true),
		Instructions:       openai.String(DeveloperInstructions),
		PreviousResponseID: openai.String(previousResponseID),
		Input:              responses.ResponseNewParamsInputUnion{OfInputItemList: c.buildInput(userPrompt, images, "solve")},
		Text: responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
			Name:        "solved_tasks",
			Description: openai.String("Финальный ответ по задачам"),
			Strict:      openai.Bool(true),
			Schema:      solveSchema(),
		}}},
	}
}

func (c *Client) buildInput(userPrompt string, images []ImageInput, stage string) responses.ResponseInputParam {
	content := make(responses.ResponseInputMessageContentListParam, 0, len(images)+1)
	content = append(content, responses.ResponseInputContentUnionParam{OfInputText: &responses.ResponseInputTextParam{Text: stagePrompt(stage, len(images), userPrompt)}})

	detail := responses.ResponseInputImageDetail(c.config.OpenAI.Detail)
	for _, image := range images {
		encoded := base64.StdEncoding.EncodeToString(image.Data)
		dataURL := fmt.Sprintf("data:%s;base64,%s", image.MimeType, encoded)
		content = append(content, responses.ResponseInputContentUnionParam{OfInputImage: &responses.ResponseInputImageParam{ImageURL: openai.String(dataURL), Detail: detail}})
	}

	return responses.ResponseInputParam{{
		OfMessage: &responses.EasyInputMessageParam{
			Role:    responses.EasyInputMessageRoleUser,
			Type:    responses.EasyInputMessageTypeMessage,
			Content: responses.EasyInputMessageContentUnionParam{OfInputItemContentList: content},
		},
	}}
}

func stagePrompt(stage string, pages int, prompt string) string {
	return fmt.Sprintf("Этап: %s. Количество изображений: %d. %s", stage, pages, prompt)
}

func (c *Client) outputTokensForPages(pages int) int64 {
	var recommended int64
	switch {
	case pages <= 2:
		recommended = 30000
	case pages <= 4:
		recommended = 40000
	default:
		recommended = 50000
	}
	cfg := int64(c.config.OpenAI.Tokens)
	if cfg > recommended {
		return cfg
	}
	return recommended
}

func (c *Client) validateSolvePayload(solve *SolveResult, extracted extractedPayload) error {
	optionsByNumber := make(map[string]map[string]struct{}, len(extracted.Tasks))
	for _, task := range extracted.Tasks {
		set := map[string]struct{}{}
		for _, opt := range task.Options {
			set[strings.TrimSpace(opt)] = struct{}{}
		}
		optionsByNumber[strings.TrimSpace(task.Number)] = set
	}

	for _, task := range solve.Tasks {
		number := strings.TrimSpace(task.Number)
		if _, ok := optionsByNumber[number]; !ok {
			return fmt.Errorf("в ответе есть несуществующий номер задачи: %s", task.Number)
		}

		if task.Status == "unreadable" && len(task.UnreadableFragments) == 0 {
			return fmt.Errorf("задача %s: статус unreadable требует непустой unreadable_fragments", task.Number)
		}

		for _, marker := range latexMarkers {
			if marker.MatchString(task.AnswerText) || marker.MatchString(task.Explanation) {
				return fmt.Errorf("задача %s: найден запрещённый LaTeX-маркер", task.Number)
			}
		}

		allowedOptions := optionsByNumber[number]
		for _, selected := range task.SelectedOptions {
			candidate := strings.TrimSpace(selected)
			if candidate == "" {
				continue
			}
			if _, ok := allowedOptions[candidate]; !ok {
				return fmt.Errorf("задача %s: выбранный вариант %q отсутствует на фото", task.Number, selected)
			}
		}
	}

	return nil
}

func extractSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"number":               map[string]any{"type": "string"},
						"task_text":            map[string]any{"type": "string"},
						"options":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"source_images":        map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
						"unreadable_fragments": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required":             []string{"number", "task_text", "options", "source_images", "unreadable_fragments"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"tasks"},
		"additionalProperties": false,
	}
}

func solveSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"number":               map[string]any{"type": "string"},
						"status":               map[string]any{"type": "string", "enum": []string{"solved", "unreadable", "partial"}},
						"selected_options":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"answer_text":          map[string]any{"type": "string"},
						"explanation":          map[string]any{"type": "string"},
						"unreadable_fragments": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required":             []string{"number", "status", "selected_options", "answer_text", "explanation", "unreadable_fragments"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"tasks"},
		"additionalProperties": false,
	}
}
