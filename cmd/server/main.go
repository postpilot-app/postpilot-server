package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/config"
	"github.com/postpilot-dev/postpilot-server/internal/handler"
	"github.com/postpilot-dev/postpilot-server/internal/model"
	"github.com/postpilot-dev/postpilot-server/internal/service"
	"github.com/postpilot-dev/postpilot-server/internal/service/ai"
	"github.com/postpilot-dev/postpilot-server/internal/service/platform"
	"github.com/postpilot-dev/postpilot-server/internal/service/storage"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "config file path")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	log.Printf("starting %s in %s mode (port: %d)", cfg.App.Name, cfg.App.Mode, cfg.App.Port)

	// 初始化数据库
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}

	// 自动迁移表结构
	if err := db.AutoMigrate(
		&model.User{},
		&model.UserAIKey{},
		&model.PlatformAccount{},
		&model.PublishRecord{},
		&model.PublishDetail{},
	); err != nil {
		log.Fatalf("auto migrate: %v", err)
	}
	log.Println("database migrated")

	// 初始化 S3
	s3Service, err := storage.NewS3Service(cfg.S3)
	if err != nil {
		log.Printf("warning: S3 not configured: %v", err)
		s3Service = nil
	}

	// 初始化 AI 服务
	aiService := ai.NewService(cfg)

	// 初始化 Meta API client (IG/FB/Threads 共用)
	metaClient := platform.NewMetaClient()

	// 初始化发布服务
	publisherService := service.NewPublisherService(cfg)

	// 初始化 Auth handler (OAuth 回调后存储 token)
	authHandler := handler.NewAuthHandler(cfg, metaClient)

	// 自用模式: 如果配置中有 Meta Token，可直接注册 publishers
	// 正常流程: 用户通过 /auth/meta/login 授权后，动态注册 publishers
	// 这里设置一个回调，在 OAuth 成功后自动注册 publishers
	go watchAndRegisterPublishers(authHandler, publisherService, metaClient)

	// 初始化 handlers
	uploadHandler := handler.NewUploadHandler(s3Service)
	aiHandler := handler.NewAIHandler(aiService)
	publishHandler := handler.NewPublishHandler(publisherService)

	// 设置 Gin
	if cfg.App.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}
	engine := gin.Default()

	// CORS 中间件
	engine.Use(corsMiddleware())

	// 注册路由
	router := handler.NewRouter(cfg, uploadHandler, aiHandler, publishHandler, authHandler)
	router.Setup(engine)

	// 启动服务
	addr := fmt.Sprintf(":%d", cfg.App.Port)
	log.Printf("server listening on %s", addr)
	if err := engine.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// watchAndRegisterPublishers polls auth tokens and registers platform publishers
func watchAndRegisterPublishers(auth *handler.AuthHandler, pub *service.PublisherService, client *platform.MetaClient) {
	// 简单轮询: 当 OAuth 完成后注册 publishers
	// 后续可改为事件驱动
	ticker := time.NewTicker(5 * time.Second)
	registered := false

	for range ticker.C {
		if registered {
			continue
		}
		tokens := auth.Tokens
		if tokens.PageToken == "" {
			continue
		}

		// Register Facebook
		pub.RegisterPlatform("facebook", platform.NewFacebookPublisher(client, tokens.PageID, tokens.PageToken))
		log.Printf("[Platform] facebook registered (page: %s)", tokens.PageName)

		// Register Instagram (if IG business account exists)
		if tokens.IGUserID != "" {
			pub.RegisterPlatform("instagram", platform.NewInstagramPublisher(client, tokens.IGUserID, tokens.PageToken))
			log.Printf("[Platform] instagram registered (ig_user: %s)", tokens.IGUserID)
		}

		// Register Threads (if Threads user exists)
		if tokens.ThreadsUID != "" {
			pub.RegisterPlatform("threads", platform.NewThreadsPublisher(client, tokens.ThreadsUID, tokens.UserToken))
			log.Printf("[Platform] threads registered (threads_uid: %s)", tokens.ThreadsUID)
		}

		registered = true
		ticker.Stop()
		log.Println("[Platform] all available platforms registered")
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
