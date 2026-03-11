package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/model"
	"github.com/postpilot-dev/postpilot-server/internal/service/storage"
	"gorm.io/gorm"
)

type PostHandler struct {
	db      *gorm.DB
	storage *storage.S3Service
}

func NewPostHandler(db *gorm.DB, s *storage.S3Service) *PostHandler {
	return &PostHandler{db: db, storage: s}
}

// ListPosts returns all posts
// GET /api/v1/posts
func (h *PostHandler) ListPosts(c *gin.Context) {
	var posts []model.Post
	if err := h.db.Where("user_id = ?", 0).Order("created_at DESC").Find(&posts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"posts": posts, "count": len(posts)})
}

// DeletePost deletes a post and its S3 files
// DELETE /api/v1/posts/:id
func (h *PostHandler) DeletePost(c *gin.Context) {
	id := c.Param("id")
	var post model.Post
	if err := h.db.First(&post, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
		return
	}

	// Delete S3 files
	var keys []string
	if err := json.Unmarshal([]byte(post.MediaKeys), &keys); err == nil && len(keys) > 0 {
		if err := h.storage.DeleteFiles(c.Request.Context(), keys); err != nil {
			log.Printf("[Post] delete S3 files: %v", err)
		}
	}

	// Delete DB record
	if err := h.db.Delete(&post).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// CleanupDrafts deletes draft posts older than specified hours and their S3 files
func (h *PostHandler) CleanupDrafts(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	var posts []model.Post
	if err := h.db.Where("status = ? AND created_at < ?", "draft", cutoff).Find(&posts).Error; err != nil {
		log.Printf("[Cleanup] query drafts: %v", err)
		return
	}

	if len(posts) == 0 {
		return
	}

	log.Printf("[Cleanup] found %d expired drafts", len(posts))
	for _, post := range posts {
		var keys []string
		if err := json.Unmarshal([]byte(post.MediaKeys), &keys); err == nil && len(keys) > 0 {
			if err := h.storage.DeleteFiles(context.Background(), keys); err != nil {
				log.Printf("[Cleanup] delete S3 files for post %d: %v", post.ID, err)
			}
		}
		h.db.Delete(&post)
	}
	log.Printf("[Cleanup] cleaned %d expired drafts", len(posts))
}
