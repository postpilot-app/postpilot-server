package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/service"
)

type PublishHandler struct {
	publisher *service.PublisherService
}

func NewPublishHandler(p *service.PublisherService) *PublishHandler {
	return &PublishHandler{publisher: p}
}

// PublishRequest 发布请求
type PublishRequest struct {
	ImageURLs []string          `json:"image_urls" binding:"required,min=1"`
	Captions  map[string]string `json:"captions" binding:"required"` // platform -> caption
	Platforms []string          `json:"platforms" binding:"required,min=1"`
}

// Publish handles multi-platform publishing
// POST /api/v1/publish
func (h *PublishHandler) Publish(c *gin.Context) {
	var req PublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 验证请求的平台都有对应的 caption
	for _, p := range req.Platforms {
		if _, ok := req.Captions[p]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing caption for platform: " + p})
			return
		}
	}

	// TODO: Phase B - 从 JWT 中获取 user_id
	result, err := h.publisher.PublishAll(c.Request.Context(), service.PublishInput{
		ImageURLs: req.ImageURLs,
		Captions:  req.Captions,
		Platforms: req.Platforms,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
