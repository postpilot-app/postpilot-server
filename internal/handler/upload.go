package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/model"
	"github.com/postpilot-dev/postpilot-server/internal/service/storage"
	"gorm.io/gorm"
)

type UploadHandler struct {
	storage *storage.S3Service
	db      *gorm.DB
}

func NewUploadHandler(s *storage.S3Service, db *gorm.DB) *UploadHandler {
	return &UploadHandler{storage: s, db: db}
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
	// Also accept "file" field for single-file uploads
	if len(files) == 0 {
		files = form.File["file"]
	}
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no images provided"})
		return
	}
	if len(files) > 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max 10 images per request"})
		return
	}

	var urls []string
	var keys []string
	for _, file := range files {
		url, key, err := h.storage.UploadFileReturnKey(c.Request.Context(), file)
		if err != nil {
			// Clean up already uploaded files on failure
			if len(keys) > 0 {
				_ = h.storage.DeleteFiles(c.Request.Context(), keys)
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed: " + err.Error()})
			return
		}
		urls = append(urls, url)
		keys = append(keys, key)
	}

	// Create Post record (draft)
	urlsJSON, _ := json.Marshal(urls)
	keysJSON, _ := json.Marshal(keys)
	post := model.Post{
		UserID:         0, // self mode
		Status:         "draft",
		MediaURLs:      string(urlsJSON),
		MediaKeys:      string(keysJSON),
		Captions:       "{}",
		Hashtags:       "[]",
		Platforms:      "[]",
		PublishResults: "{}",
	}
	if err := h.db.Create(&post).Error; err != nil {
		log.Printf("[Upload] create post record: %v", err)
	}

	resp := gin.H{
		"urls":    urls,
		"count":   len(urls),
		"post_id": post.ID,
	}
	if len(urls) == 1 {
		resp["url"] = urls[0]
	}
	c.JSON(http.StatusOK, resp)
}
