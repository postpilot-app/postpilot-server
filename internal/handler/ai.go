package handler

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/service/ai"
)

type AIHandler struct {
	aiService *ai.Service
}

func NewAIHandler(s *ai.Service) *AIHandler {
	return &AIHandler{aiService: s}
}

// GenerateRequest AI 文案生成请求
type GenerateRequest struct {
	ImageURLs []string `json:"image_urls" binding:"required,min=1"`
	Prompt    string   `json:"prompt"`
	Languages []string `json:"languages" binding:"required,min=1"`
	Platforms []string `json:"platforms" binding:"required,min=1"`
	Style     string   `json:"style"` // casual, professional, humorous
}

// Generate handles AI caption generation
// POST /api/v1/ai/generate
func (h *AIHandler) Generate(c *gin.Context) {
	var req GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[AI/Generate] bind error: %v, content-type: %s", err, c.ContentType())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[AI/Generate] request: urls=%d, langs=%v, platforms=%v, style=%s", len(req.ImageURLs), req.Languages, req.Platforms, req.Style)

	if req.Style == "" {
		req.Style = "casual"
	}

	result, err := h.aiService.GenerateCaptions(c.Request.Context(), ai.GenerateInput{
		ImageURLs: req.ImageURLs,
		Prompt:    req.Prompt,
		Languages: req.Languages,
		Platforms: req.Platforms,
		Style:     req.Style,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "generation failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
