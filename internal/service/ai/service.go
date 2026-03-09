package ai

import (
	"context"
	"fmt"
	"log"

	"github.com/postpilot-dev/postpilot-server/internal/config"
)

// GenerateInput AI 文案生成输入
type GenerateInput struct {
	ImageURLs []string
	Prompt    string
	Languages []string
	Platforms []string
	Style     string
	// Phase B: UserID int64
}

// GenerateOutput AI 文案生成输出
type GenerateOutput struct {
	Captions map[string]map[string]string `json:"captions"`      // platform -> language -> caption
	Hashtags []string                     `json:"hashtags"`
	Provider string                       `json:"provider_used"`
}

// Provider AI 提供商接口
type Provider interface {
	Generate(ctx context.Context, input GenerateInput) (*GenerateOutput, error)
	Name() string
}

// Service AI 服务 (Gemini 主力 → Claude 兜底，与 zsport-crawler 一致)
type Service struct {
	cfg       *config.Config
	providers []Provider // 按优先级排序
}

func NewService(cfg *config.Config) *Service {
	s := &Service{cfg: cfg}

	// Gemini 主力
	if cfg.AI.Gemini.APIKey != "" {
		s.providers = append(s.providers, NewGeminiProvider(cfg.AI.Gemini))
		log.Printf("[AI] gemini registered (models: %v)", cfg.AI.Gemini.Models)
	}

	// Claude 兜底
	if cfg.AI.Claude.APIKey != "" {
		s.providers = append(s.providers, NewClaudeProvider(cfg.AI.Claude))
		log.Printf("[AI] claude registered as fallback (models: %v)", cfg.AI.Claude.Models)
	}

	if len(s.providers) == 0 {
		log.Println("[AI] warning: no AI providers configured")
	} else {
		names := make([]string, len(s.providers))
		for i, p := range s.providers {
			names[i] = p.Name()
		}
		log.Printf("[AI] provider chain: %s", joinNames(names))
	}

	return s
}

// GenerateCaptions Gemini 优先，失败自动 fallback 到 Claude
func (s *Service) GenerateCaptions(ctx context.Context, input GenerateInput) (*GenerateOutput, error) {
	if len(s.providers) == 0 {
		return nil, fmt.Errorf("no AI provider configured, set GEMINI_API_KEY")
	}

	var lastErr error
	for _, p := range s.providers {
		output, err := p.Generate(ctx, input)
		if err != nil {
			log.Printf("[AI] %s failed: %v", p.Name(), err)
			lastErr = err
			if p != s.providers[len(s.providers)-1] {
				log.Printf("[AI] falling back to next provider")
			}
			continue
		}
		output.Provider = p.Name()
		return output, nil
	}

	return nil, fmt.Errorf("all AI providers failed: %w", lastErr)
}

func joinNames(names []string) string {
	result := names[0]
	for _, n := range names[1:] {
		result += " → " + n
	}
	return result
}
