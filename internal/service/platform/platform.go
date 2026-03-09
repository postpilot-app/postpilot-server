package platform

import "context"

// PublishInput 平台发布输入
type PublishInput struct {
	ImageURLs []string
	Caption   string
}

// Publisher 平台发布器接口
type Publisher interface {
	Publish(ctx context.Context, input PublishInput) PlatformResult
	Name() string
}

// PlatformResult 平台发布结果 (与 service.PlatformResult 对应)
type PlatformResult struct {
	Platform string `json:"platform"`
	Status   string `json:"status"`
	PostID   string `json:"post_id,omitempty"`
	PostURL  string `json:"post_url,omitempty"`
	Error    string `json:"error,omitempty"`
}
