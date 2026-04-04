package openaiagent

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testsolverbot/internal/config"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
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

func (c *Client) StreamSolveImage(ctx context.Context, image ImageInput, onDelta func(string)) (string, error) {
	content := make([]openai.ChatCompletionContentPartUnionParam, 0, 2)
	content = append(content, openai.TextContentPart(Prompt))
	encoded := base64.StdEncoding.EncodeToString(image.Data)
	dataURL := fmt.Sprintf("data:%s;base64,%s", image.MimeType, encoded)
	content = append(content, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
		URL:    dataURL,
		Detail: c.config.OpenAI.Detail,
	}))

	stream := c.cli.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:               c.config.OpenAI.Model,
		ReasoningEffort:     shared.ReasoningEffort(c.config.OpenAI.Reason),
		MaxCompletionTokens: openai.Int(int64(c.config.OpenAI.Tokens)),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(content),
		},
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	})
	defer stream.Close()

	var out strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		for _, choice := range chunk.Choices {
			delta := choice.Delta.Content
			if delta == "" {
				continue
			}
			out.WriteString(delta)
			onDelta(delta)
		}
	}
	if err := stream.Err(); err != nil {
		return "", err
	}
	return out.String(), nil
}
