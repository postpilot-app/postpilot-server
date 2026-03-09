package platform

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"time"
)

type InstagramPublisher struct {
	client      *MetaClient
	igUserID    string // Instagram Business Account ID
	accessToken string
}

func NewInstagramPublisher(client *MetaClient, igUserID, accessToken string) *InstagramPublisher {
	return &InstagramPublisher{
		client:      client,
		igUserID:    igUserID,
		accessToken: accessToken,
	}
}

func (p *InstagramPublisher) Name() string { return "instagram" }

func (p *InstagramPublisher) Publish(ctx context.Context, input PublishInput) PlatformResult {
	if len(input.ImageURLs) == 0 {
		return PlatformResult{Platform: "instagram", Status: "failed", Error: "no images provided"}
	}

	var postID string
	var err error

	if len(input.ImageURLs) == 1 {
		postID, err = p.publishSingle(ctx, input.ImageURLs[0], input.Caption)
	} else {
		postID, err = p.publishCarousel(ctx, input.ImageURLs, input.Caption)
	}

	if err != nil {
		log.Printf("[IG] publish failed: %v", err)
		return PlatformResult{Platform: "instagram", Status: "failed", Error: err.Error()}
	}

	log.Printf("[IG] published successfully: %s", postID)
	return PlatformResult{
		Platform: "instagram",
		Status:   "success",
		PostID:   postID,
		PostURL:  fmt.Sprintf("https://www.instagram.com/p/%s", postID),
	}
}

// publishSingle: single image post (2-step)
func (p *InstagramPublisher) publishSingle(ctx context.Context, imageURL, caption string) (string, error) {
	// Step 1: Create media container
	params := url.Values{
		"image_url":    {imageURL},
		"caption":      {caption},
		"access_token": {p.accessToken},
	}
	result, err := p.client.graphPost(ctx, "/"+p.igUserID+"/media", params)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	containerID, _ := result["id"].(string)
	if containerID == "" {
		return "", fmt.Errorf("no container ID returned")
	}

	// Wait for container to be ready
	if err := p.waitForContainer(ctx, containerID); err != nil {
		return "", err
	}

	// Step 2: Publish
	return p.publishContainer(ctx, containerID)
}

// publishCarousel: carousel post (3-step)
func (p *InstagramPublisher) publishCarousel(ctx context.Context, imageURLs []string, caption string) (string, error) {
	// Step 1: Create individual item containers
	var childIDs []string
	for _, imgURL := range imageURLs {
		params := url.Values{
			"image_url":    {imgURL},
			"is_carousel_item": {"true"},
			"access_token": {p.accessToken},
		}
		result, err := p.client.graphPost(ctx, "/"+p.igUserID+"/media", params)
		if err != nil {
			return "", fmt.Errorf("create carousel item: %w", err)
		}
		childID, _ := result["id"].(string)
		if childID == "" {
			return "", fmt.Errorf("no carousel item ID returned")
		}
		childIDs = append(childIDs, childID)
	}

	// Wait for all child containers
	for _, id := range childIDs {
		if err := p.waitForContainer(ctx, id); err != nil {
			return "", fmt.Errorf("child container %s: %w", id, err)
		}
	}

	// Step 2: Create carousel container
	params := url.Values{
		"media_type":   {"CAROUSEL"},
		"caption":      {caption},
		"access_token": {p.accessToken},
	}
	for _, id := range childIDs {
		params.Add("children", id)
	}
	result, err := p.client.graphPost(ctx, "/"+p.igUserID+"/media", params)
	if err != nil {
		return "", fmt.Errorf("create carousel container: %w", err)
	}
	carouselID, _ := result["id"].(string)

	// Wait for carousel container
	if err := p.waitForContainer(ctx, carouselID); err != nil {
		return "", err
	}

	// Step 3: Publish
	return p.publishContainer(ctx, carouselID)
}

func (p *InstagramPublisher) publishContainer(ctx context.Context, containerID string) (string, error) {
	params := url.Values{
		"creation_id":  {containerID},
		"access_token": {p.accessToken},
	}
	result, err := p.client.graphPost(ctx, "/"+p.igUserID+"/media_publish", params)
	if err != nil {
		return "", fmt.Errorf("publish: %w", err)
	}
	postID, _ := result["id"].(string)
	return postID, nil
}

func (p *InstagramPublisher) waitForContainer(ctx context.Context, containerID string) error {
	for i := 0; i < 30; i++ {
		params := url.Values{
			"fields":       {"status_code"},
			"access_token": {p.accessToken},
		}
		result, err := p.client.graphGet(ctx, "/"+containerID, params)
		if err != nil {
			return fmt.Errorf("check container status: %w", err)
		}

		status, _ := result["status_code"].(string)
		switch status {
		case "FINISHED":
			return nil
		case "ERROR":
			return fmt.Errorf("container processing failed")
		default:
			// IN_PROGRESS or other
			time.Sleep(2 * time.Second)
		}
	}
	return fmt.Errorf("container processing timeout (60s)")
}
