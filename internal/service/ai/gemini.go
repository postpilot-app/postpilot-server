package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/postpilot-dev/postpilot-server/internal/config"
	"google.golang.org/genai"
)

type GeminiProvider struct {
	apiKey string
	models []string // 按优先级排序，前面的挂了试下一个
}

func NewGeminiProvider(cfg config.GeminiConfig) *GeminiProvider {
	return &GeminiProvider{
		apiKey: cfg.APIKey,
		models: cfg.Models,
	}
}

func (g *GeminiProvider) Name() string { return "gemini" }

func (g *GeminiProvider) Generate(ctx context.Context, input GenerateInput) (*GenerateOutput, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  g.apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}

	prompt := buildPrompt(input)

	// 构建请求内容
	parts := []*genai.Part{genai.NewPartFromText(prompt)}

	// 下载图片并以 inline bytes 发送
	for _, imgURL := range input.ImageURLs {
		data, mimeType, err := downloadImage(ctx, imgURL)
		if err != nil {
			return nil, fmt.Errorf("download image %s: %w", imgURL, err)
		}
		parts = append(parts, genai.NewPartFromBytes(data, mimeType))
	}

	content := []*genai.Content{{Role: "user", Parts: parts}}
	temp := float32(0.8)
	var maxTokens int32 = 4096
	cfg := &genai.GenerateContentConfig{
		Temperature:      &temp,
		MaxOutputTokens:  maxTokens,
		ResponseMIMEType: "application/json",
	}

	// 逐个模型尝试
	var lastErr error
	for _, model := range g.models {
		resp, err := client.Models.GenerateContent(ctx, model, content, cfg)
		if err != nil {
			log.Printf("[AI] gemini model %s failed: %v", model, err)
			lastErr = err
			continue
		}

		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			lastErr = fmt.Errorf("model %s returned empty response", model)
			log.Printf("[AI] %v", lastErr)
			continue
		}

		text := resp.Candidates[0].Content.Parts[0].Text
		if text == "" {
			lastErr = fmt.Errorf("model %s returned non-text response", model)
			log.Printf("[AI] %v", lastErr)
			continue
		}

		log.Printf("[AI] gemini %s raw response (len=%d): %.200s", model, len(text), text)

		var output GenerateOutput
		if err := json.Unmarshal([]byte(text), &output); err != nil {
			lastErr = fmt.Errorf("parse %s response: %w", model, err)
			log.Printf("[AI] %v", lastErr)
			continue
		}

		log.Printf("[AI] gemini success with model: %s", model)
		return &output, nil
	}

	return nil, fmt.Errorf("all gemini models failed: %w", lastErr)
}

func buildPrompt(input GenerateInput) string {
	var sb strings.Builder

	sb.WriteString(`你是一个专业的社交媒体文案专家，擅长为不同平台撰写吸引人的文案。

根据用户提供的照片和描述，为指定平台生成文案。

要求:
1. 每个平台的文案风格需符合该平台特点:
   - Instagram: 简洁有力，多用 Emoji，附带 5-10 个相关 hashtag
   - Facebook: 可以稍长，叙事性强，引发互动
   - Threads: 对话性强，观点输出，适度 hashtag
2. 必须包含合适的 Emoji
3. 文案长度适中，不要过长

`)

	sb.WriteString(fmt.Sprintf("用户描述: %s\n", input.Prompt))
	sb.WriteString(fmt.Sprintf("风格要求: %s\n", styleDescription(input.Style)))
	sb.WriteString(fmt.Sprintf("目标平台: %s\n", strings.Join(input.Platforms, ", ")))
	sb.WriteString(fmt.Sprintf("目标语言: %s\n", strings.Join(input.Languages, ", ")))

	sb.WriteString(`
请以以下 JSON 格式输出:
{
  "captions": {
    "<platform>": {
      "<language_code>": "<caption text>"
    }
  },
  "hashtags": ["#tag1", "#tag2", ...]
}

language_code 使用: zh=中文, en=English, th=ภาษาไทย

只输出 JSON，不要有其他内容。`)

	return sb.String()
}

// downloadImage 下载图片并返回字节和 MIME 类型
func downloadImage(ctx context.Context, url string) ([]byte, string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024)) // 20MB max
	if err != nil {
		return nil, "", err
	}

	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" || !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/jpeg"
	}

	return data, mimeType, nil
}

func styleDescription(style string) string {
	switch style {
	case "professional":
		return "专业正式，适合品牌或商业内容"
	case "humorous":
		return "幽默诙谐，轻松有趣"
	default:
		return "随意自然，像朋友分享日常"
	}
}
