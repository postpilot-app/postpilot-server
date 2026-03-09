package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	App        AppConfig      `mapstructure:"app"`
	Database   DatabaseConfig `mapstructure:"database"`
	AI         AIConfig       `mapstructure:"ai"`
	S3         S3Config       `mapstructure:"s3"`
	Meta       MetaConfig     `mapstructure:"meta"`
	Auth       AuthConfig     `mapstructure:"auth"`
	Encryption EncryptConfig  `mapstructure:"encryption"`
}

type AppConfig struct {
	Name   string `mapstructure:"name"`
	Mode   string `mapstructure:"mode"`    // "self" or "multi"
	Port   int    `mapstructure:"port"`
	Env    string `mapstructure:"env"`
	APIKey string `mapstructure:"api_key"` // self 模式下的 API 访问密钥
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	Charset  string `mapstructure:"charset"`
}

func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		d.User, d.Password, d.Host, d.Port, d.DBName, d.Charset)
}

type AIConfig struct {
	DefaultProvider string       `mapstructure:"default_provider"`
	Gemini          GeminiConfig `mapstructure:"gemini"`
	Claude          ClaudeConfig `mapstructure:"claude"`
}

type GeminiConfig struct {
	APIKey string   `mapstructure:"api_key"`
	Models []string `mapstructure:"models"`
}

type ClaudeConfig struct {
	APIKey string   `mapstructure:"api_key"`
	Models []string `mapstructure:"models"`
}

type S3Config struct {
	Region    string `mapstructure:"region"`
	Bucket    string `mapstructure:"bucket"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	CDNURL    string `mapstructure:"cdn_url"`
}

type MetaConfig struct {
	AppID       string `mapstructure:"app_id"`
	AppSecret   string `mapstructure:"app_secret"`
	RedirectURI string `mapstructure:"redirect_uri"`
}

type AuthConfig struct {
	JWTSecret   string `mapstructure:"jwt_secret"`
	TokenExpire string `mapstructure:"token_expire"`
}

type EncryptConfig struct {
	AESKey string `mapstructure:"aes_key"`
}

func (c *Config) IsSelfMode() bool {
	return c.App.Mode == "self"
}

func Load(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// 支持环境变量覆盖，前缀 POSTPILOT_
	v.SetEnvPrefix("POSTPILOT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// 环境变量覆盖敏感配置
	if key := os.Getenv("POSTPILOT_AES_KEY"); key != "" {
		cfg.Encryption.AESKey = key
	}

	// Self 模式 API Key: POSTPILOT_API_KEY
	if cfg.App.APIKey == "" {
		if key := os.Getenv("POSTPILOT_API_KEY"); key != "" {
			cfg.App.APIKey = key
		}
	}

	// Gemini: GEMINI_API_KEY (与 zsport-crawler 共用)
	if cfg.AI.Gemini.APIKey == "" {
		if key := os.Getenv("GEMINI_API_KEY"); key != "" {
			cfg.AI.Gemini.APIKey = key
		}
	}

	// Claude: ANTHROPIC_API_KEY (与 zsport-crawler 共用，仅做兜底)
	if cfg.AI.Claude.APIKey == "" {
		if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			cfg.AI.Claude.APIKey = key
		}
	}

	return &cfg, nil
}
