package model

import "time"

// User 用户表
type User struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string    `gorm:"size:50;uniqueIndex;not null" json:"username"`
	Email        string    `gorm:"size:100;uniqueIndex" json:"email,omitempty"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	AvatarURL    string    `gorm:"size:500" json:"avatar_url,omitempty"`
	Status       int8      `gorm:"default:1" json:"status"` // 1=active, 0=disabled
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserAIKey 用户 AI API Key 表
type UserAIKey struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID          int64     `gorm:"not null;uniqueIndex:uk_user_provider" json:"user_id"`
	Provider        string    `gorm:"size:20;not null;uniqueIndex:uk_user_provider" json:"provider"` // gemini, claude, openai
	APIKeyEncrypted string    `gorm:"type:text;not null" json:"-"`
	ModelName       string    `gorm:"size:50" json:"model_name"`
	IsDefault       bool      `gorm:"default:false" json:"is_default"`
	IsValid         bool      `gorm:"default:true" json:"is_valid"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PlatformAccount 平台授权表
type PlatformAccount struct {
	ID                    int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID                int64     `gorm:"not null;uniqueIndex:uk_user_platform" json:"user_id"`
	Platform              string    `gorm:"size:20;not null;uniqueIndex:uk_user_platform" json:"platform"` // instagram, facebook, threads
	AccountName           string    `gorm:"size:100" json:"account_name"`
	AccessTokenEncrypted  string    `gorm:"type:text;not null" json:"-"`
	RefreshTokenEncrypted string    `gorm:"type:text" json:"-"`
	TokenExpiresAt        *time.Time `json:"token_expires_at,omitempty"`
	ExtraData             string    `gorm:"type:json" json:"extra_data,omitempty"` // page_id, ig_user_id, etc.
	IsValid               bool      `gorm:"default:true" json:"is_valid"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// PublishRecord 发布记录表
type PublishRecord struct {
	ID               int64            `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID           int64            `gorm:"not null;index" json:"user_id"`
	ImageURLs        string           `gorm:"type:json;not null" json:"image_urls"`
	Prompt           string           `gorm:"type:text" json:"prompt,omitempty"`
	GeneratedCaption string           `gorm:"type:text" json:"generated_caption,omitempty"`
	AIProvider       string           `gorm:"size:20" json:"ai_provider,omitempty"`
	Status           string           `gorm:"size:20;default:pending" json:"status"` // pending, publishing, partial, success, failed
	Details          []PublishDetail  `gorm:"foreignKey:RecordID" json:"details,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
}

// PublishDetail 发布详情表
type PublishDetail struct {
	ID              int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	RecordID        int64      `gorm:"not null;index" json:"record_id"`
	Platform        string     `gorm:"size:20;not null" json:"platform"`
	Caption         string     `gorm:"type:text" json:"caption,omitempty"`
	Status          string     `gorm:"size:20;default:pending" json:"status"` // pending, publishing, success, failed
	PlatformPostID  string     `gorm:"size:100" json:"platform_post_id,omitempty"`
	PlatformPostURL string     `gorm:"size:500" json:"platform_post_url,omitempty"`
	ErrorMessage    string     `gorm:"type:text" json:"error_message,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}
