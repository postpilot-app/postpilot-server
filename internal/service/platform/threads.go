package platform

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

type ThreadsPublisher struct {
	client       *MetaClient
	threadsUID   string // Threads User ID
	accessToken  string
}

func NewThreadsPublisher(client *MetaClient, threadsUID, accessToken string) *ThreadsPublisher {
	return &ThreadsPublisher{
		client:      client,
		threadsUID:  threadsUID,
		accessToken: accessToken,
	}
}

func (p *ThreadsPublisher) Name() string { return "threads" }

func (p *ThreadsPublisher) Publish(ctx context.Context, input PublishInput) PlatformResult {
	if len(input.ImageURLs) == 0 {
		return PlatformResult{Platform: "threads", Status: "failed", Error: "no images provided"}
	}

	var postID string
	var err error

	if len(input.ImageURLs) == 1 {
		postID, err = p.publishSingle(ctx, input.ImageURLs[0], input.Caption)
	} else {
		postID, err = p.publishCarousel(ctx, input.ImageURLs, input.Caption)
	}

	if err != nil {
		log.Printf("[Threads] publish failed: %v", err)
		return PlatformResult{Platform: "threads", Status: "failed", Error: err.Error()}
	}

	log.Printf("[Threads] published successfully: %s", postID)
	return PlatformResult{
		Platform: "threads",
		Status:   "success",
		PostID:   postID,
		PostURL:  fmt.Sprintf("https://www.threads.net/post/%s", postID),
	}
}

// publishSingle: single image post (2-step)
func (p *ThreadsPublisher) publishSingle(ctx context.Context, imageURL, caption string) (string, error) {
	// Step 1: Create media container
	params := url.Values{
		"media_type":   {"IMAGE"},
		"image_url":    {imageURL},
		"text":         {caption},
		"access_token": {p.accessToken},
	}
	result, err := p.client.graphPost(ctx, "/"+p.threadsUID+"/threads", params)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	containerID, _ := result["id"].(string)
	if containerID == "" {
		return "", fmt.Errorf("no container ID returned")
	}

	// Wait for container
	if err := p.waitForContainer(ctx, containerID); err != nil {
		return "", err
	}

	// Step 2: Publish
	return p.publishContainer(ctx, containerID)
}

// publishCarousel: multi-image post (3-step)
func (p *ThreadsPublisher) publishCarousel(ctx context.Context, imageURLs []string, caption string) (string, error) {
	// Step 1: Create individual item containers
	var childIDs []string
	for _, imgURL := range imageURLs {
		params := url.Values{
			"media_type":       {"IMAGE"},
			"image_url":        {imgURL},
			"is_carousel_item": {"true"},
			"access_token":     {p.accessToken},
		}
		result, err := p.client.graphPost(ctx, "/"+p.threadsUID+"/threads", params)
		if err != nil {
			return "", fmt.Errorf("create carousel item: %w", err)
		}
		childID, _ := result["id"].(string)
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
		"text":         {caption},
		"children":     {strings.Join(childIDs, ",")},
		"access_token": {p.accessToken},
	}
	result, err := p.client.graphPost(ctx, "/"+p.threadsUID+"/threads", params)
	if err != nil {
		return "", fmt.Errorf("create carousel: %w", err)
	}
	carouselID, _ := result["id"].(string)

	if err := p.waitForContainer(ctx, carouselID); err != nil {
		return "", err
	}

	// Step 3: Publish
	return p.publishContainer(ctx, carouselID)
}

func (p *ThreadsPublisher) publishContainer(ctx context.Context, containerID string) (string, error) {
	params := url.Values{
		"creation_id":  {containerID},
		"access_token": {p.accessToken},
	}
	result, err := p.client.graphPost(ctx, "/"+p.threadsUID+"/threads_publish", params)
	if err != nil {
		return "", fmt.Errorf("publish: %w", err)
	}
	postID, _ := result["id"].(string)
	return postID, nil
}

func (p *ThreadsPublisher) waitForContainer(ctx context.Context, containerID string) error {
	for i := 0; i < 30; i++ {
		params := url.Values{
			"fields":       {"status"},
			"access_token": {p.accessToken},
		}
		result, err := p.client.GraphGet(ctx, "/"+containerID, params)
		if err != nil {
			return fmt.Errorf("check container status: %w", err)
		}

		status, _ := result["status"].(string)
		switch status {
		case "FINISHED":
			return nil
		case "ERROR":
			errMsg, _ := result["error_message"].(string)
			return fmt.Errorf("container processing failed: %s", errMsg)
		default:
			time.Sleep(2 * time.Second)
		}
	}
	return fmt.Errorf("container processing timeout (60s)")
}
