package util

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Environment          string        `mapstructure:"ENVIRONMENT"`
	Port                 string        `mapstructure:"PORT"`
	DatabaseURL          string        `mapstructure:"DATABASE_URL"`
	OpenAIAPIKey         string        `mapstructure:"OPENAI_API_KEY"`
	TokenSecretKey       string        `mapstructure:"TOKEN_SECRET_KEY"`
	AccessTokenDuration  time.Duration `mapstructure:"ACCESS_TOKEN_DURATION"`
	RefreshTokenDuration time.Duration `mapstructure:"REFRESH_TOKEN_DURATION"`
}

func LoadConfig(path string) (config Config, err error) {
	viper.AddConfigPath(path)  // For local config file if any (e.g. app.yaml)
	viper.SetConfigName("app") // Name of config file (app.env, app.yaml)
	viper.SetConfigType("env") // Can also read from .env file directly

	viper.AutomaticEnv() // Read environment variables

	// Set defaults
	viper.SetDefault("ENVIRONMENT", "development")
	viper.SetDefault("PORT", "8080")
	viper.SetDefault("ACCESS_TOKEN_DURATION", "15m")
	viper.SetDefault("REFRESH_TOKEN_DURATION", "168h") // 7 days

	err = viper.ReadInConfig() // Attempt to read config file (e.g., app.env if AddConfigPath and SetConfigName match)
	if err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Config file was found but another error was produced
			return
		}
		// Config file not found; ignore error if it's just not found
		// Environment variables will still be read by AutomaticEnv()
	}

	err = viper.Unmarshal(&config)
	return
}
