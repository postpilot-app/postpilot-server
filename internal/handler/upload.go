package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/service/storage"
)

type UploadHandler struct {
	storage *storage.S3Service
}

func NewUploadHandler(s *storage.S3Service) *UploadHandler {
	return &UploadHandler{storage: s}
}

// Upload handles single/multiple image uploads to S3
// POST /api/v1/upload
func (h *UploadHandler) Upload(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid multipart form"})
		return
	}

	files := form.File["images"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no images provided"})
		return
	}
	if len(files) > 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max 10 images per request"})
		return
	}

	var urls []string
	for _, file := range files {
		url, err := h.storage.UploadFile(c.Request.Context(), file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed: " + err.Error()})
			return
		}
		urls = append(urls, url)
	}

	c.JSON(http.StatusOK, gin.H{
		"urls":  urls,
		"count": len(urls),
	})
}
