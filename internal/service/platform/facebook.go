package platform

import (
	"context"
	"fmt"
	"log"
	"net/url"
)

type FacebookPublisher struct {
	client      *MetaClient
	pageID      string
	accessToken string // Page Access Token
}

func NewFacebookPublisher(client *MetaClient, pageID, accessToken string) *FacebookPublisher {
	return &FacebookPublisher{
		client:      client,
		pageID:      pageID,
		accessToken: accessToken,
	}
}

func (p *FacebookPublisher) Name() string { return "facebook" }

func (p *FacebookPublisher) Publish(ctx context.Context, input PublishInput) PlatformResult {
	if len(input.ImageURLs) == 0 {
		return PlatformResult{Platform: "facebook", Status: "failed", Error: "no images provided"}
	}

	var postID string
	var err error

	if len(input.ImageURLs) == 1 {
		postID, err = p.publishSinglePhoto(ctx, input.ImageURLs[0], input.Caption)
	} else {
		postID, err = p.publishMultiPhoto(ctx, input.ImageURLs, input.Caption)
	}

	if err != nil {
		log.Printf("[FB] publish failed: %v", err)
		return PlatformResult{Platform: "facebook", Status: "failed", Error: err.Error()}
	}

	log.Printf("[FB] published successfully: %s", postID)
	return PlatformResult{
		Platform: "facebook",
		Status:   "success",
		PostID:   postID,
		PostURL:  fmt.Sprintf("https://www.facebook.com/%s", postID),
	}
}

// publishSinglePhoto: single photo with caption
func (p *FacebookPublisher) publishSinglePhoto(ctx context.Context, imageURL, caption string) (string, error) {
	params := url.Values{
		"url":          {imageURL},
		"message":      {caption},
		"access_token": {p.accessToken},
	}
	result, err := p.client.graphPost(ctx, "/"+p.pageID+"/photos", params)
	if err != nil {
		return "", fmt.Errorf("publish photo: %w", err)
	}
	postID, _ := result["post_id"].(string)
	if postID == "" {
		// fallback to "id"
		postID, _ = result["id"].(string)
	}
	return postID, nil
}

// publishMultiPhoto: multiple photos in one post
// Step 1: Upload each photo unpublished → get photo_ids
// Step 2: Create feed post with attached_media referencing photo_ids
func (p *FacebookPublisher) publishMultiPhoto(ctx context.Context, imageURLs []string, caption string) (string, error) {
	var photoIDs []string

	// Step 1: Upload each photo as unpublished
	for _, imgURL := range imageURLs {
		params := url.Values{
			"url":          {imgURL},
			"published":    {"false"},
			"access_token": {p.accessToken},
		}
		result, err := p.client.graphPost(ctx, "/"+p.pageID+"/photos", params)
		if err != nil {
			return "", fmt.Errorf("upload unpublished photo: %w", err)
		}
		photoID, _ := result["id"].(string)
		if photoID == "" {
			return "", fmt.Errorf("no photo ID returned")
		}
		photoIDs = append(photoIDs, photoID)
	}

	// Step 2: Create feed post with attached_media
	params := url.Values{
		"message":      {caption},
		"access_token": {p.accessToken},
	}
	for i, pid := range photoIDs {
		params.Set(fmt.Sprintf("attached_media[%d]", i), fmt.Sprintf(`{"media_fbid":"%s"}`, pid))
	}
	result, err := p.client.graphPost(ctx, "/"+p.pageID+"/feed", params)
	if err != nil {
		return "", fmt.Errorf("create multi-photo post: %w", err)
	}
	postID, _ := result["id"].(string)
	return postID, nil
}
