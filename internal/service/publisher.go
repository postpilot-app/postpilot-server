package service

import (
	"context"
	"sync"

	"github.com/postpilot-dev/postpilot-server/internal/config"
	"github.com/postpilot-dev/postpilot-server/internal/service/platform"
)

// PublishInput 发布请求输入
type PublishInput struct {
	ImageURLs []string
	Captions  map[string]string // platform -> caption
	Platforms []string
	// Phase B: UserID int64
}

// PublishOutput 发布结果
type PublishOutput struct {
	Status  string                    `json:"status"` // success, partial, failed
	Results []platform.PlatformResult `json:"results"`
}

// PublisherService 发布调度引擎
type PublisherService struct {
	cfg       *config.Config
	platforms map[string]platform.Publisher
}

func NewPublisherService(cfg *config.Config) *PublisherService {
	return &PublisherService{
		cfg:       cfg,
		platforms: make(map[string]platform.Publisher),
	}
}

// RegisterPlatform 注册平台发布器
func (s *PublisherService) RegisterPlatform(name string, p platform.Publisher) {
	s.platforms[name] = p
}

// PublishAll 并发发布到多个平台
func (s *PublisherService) PublishAll(ctx context.Context, input PublishInput) (*PublishOutput, error) {
	var wg sync.WaitGroup
	resultCh := make(chan platform.PlatformResult, len(input.Platforms))

	for _, p := range input.Platforms {
		publisher, ok := s.platforms[p]
		if !ok {
			resultCh <- platform.PlatformResult{
				Platform: p,
				Status:   "failed",
				Error:    "platform not configured",
			}
			continue
		}

		wg.Add(1)
		go func(platformName string, pub platform.Publisher) {
			defer wg.Done()
			result := pub.Publish(ctx, platform.PublishInput{
				ImageURLs: input.ImageURLs,
				Caption:   input.Captions[platformName],
			})
			result.Platform = platformName
			resultCh <- result
		}(p, publisher)
	}

	wg.Wait()
	close(resultCh)

	output := &PublishOutput{}
	successCount := 0
	for r := range resultCh {
		output.Results = append(output.Results, r)
		if r.Status == "success" {
			successCount++
		}
	}

	switch {
	case successCount == len(input.Platforms):
		output.Status = "success"
	case successCount > 0:
		output.Status = "partial"
	default:
		output.Status = "failed"
	}

	return output, nil
}
