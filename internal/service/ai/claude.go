package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/postpilot-dev/postpilot-server/internal/config"
)

type ClaudeProvider struct {
	apiKey string
	models []string
}

func NewClaudeProvider(cfg config.ClaudeConfig) *ClaudeProvider {
	return &ClaudeProvider{
		apiKey: cfg.APIKey,
		models: cfg.Models,
	}
}

func (c *ClaudeProvider) Name() string { return "claude" }

func (c *ClaudeProvider) Generate(ctx context.Context, input GenerateInput) (*GenerateOutput, error) {
	client := anthropic.NewClient(option.WithAPIKey(c.apiKey))

	prompt := buildPrompt(input)

	// 构建消息内容
	content := []anthropic.ContentBlockParamUnion{
		anthropic.NewTextBlock(prompt),
	}

	// 添加图片 (通过 URL)
	for _, url := range input.ImageURLs {
		content = append(content, anthropic.NewImageBlock(anthropic.URLImageSourceParam{
			URL: url,
		}))
	}

	messages := []anthropic.MessageParam{{Role: "user", Content: content}}

	// 逐个模型尝试
	var lastErr error
	for _, model := range c.models {
		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(model),
			MaxTokens: 4096,
			Messages:  messages,
		})
		if err != nil {
			log.Printf("[AI] claude model %s failed: %v", model, err)
			lastErr = err
			continue
		}

		var text string
		for _, block := range resp.Content {
			if t := block.AsText(); t.Text != "" {
				text = t.Text
				break
			}
		}
		if text == "" {
			lastErr = fmt.Errorf("model %s returned empty response", model)
			log.Printf("[AI] %v", lastErr)
			continue
		}

		text = extractJSON(text)

		var output GenerateOutput
		if err := json.Unmarshal([]byte(text), &output); err != nil {
			lastErr = fmt.Errorf("parse %s response: %w", model, err)
			log.Printf("[AI] %v", lastErr)
			continue
		}

		log.Printf("[AI] claude success with model: %s", model)
		return &output, nil
	}

	return nil, fmt.Errorf("all claude models failed: %w", lastErr)
}

// extractJSON 从文本中提取 JSON (处理 ```json ``` 包裹等情况)
func extractJSON(text string) string {
	text = strings.TrimSpace(text)

	if start := strings.Index(text, "```json"); start != -1 {
		start += len("```json")
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	if start := strings.Index(text, "```"); start != -1 {
		start += len("```")
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	if start := strings.Index(text, "{"); start != -1 {
		if end := strings.LastIndex(text, "}"); end != -1 {
			return text[start : end+1]
		}
	}

	return text
}
