package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/shawgichan/research-service/internal/api"
	"github.com/shawgichan/research-service/internal/db"
	applogger "github.com/shawgichan/research-service/internal/logger" // aliased to avoid conflict
	"github.com/shawgichan/research-service/internal/services"
	"github.com/shawgichan/research-service/internal/token"
	"github.com/shawgichan/research-service/internal/util"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load("app.env"); err != nil {
		// Allow running without .env for containerized environments
		if !os.IsNotExist(err) {
			applogger.New().Fatal("Error loading .env file", err)
		}
	}

	// Initialize logger
	logger := applogger.New()

	// Load configuration
	config, err := util.LoadConfig(".") // Load from env vars primarily
	if err != nil {
		logger.Fatal("Cannot load config:", err)
	}

	if config.Environment == "development" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize database connection pool
	connPool, err := db.ConnectDB(config.DatabaseURL)
	if err != nil {
		logger.Fatal("Cannot connect to database:", err)
	}
	defer connPool.Close()

	// Create a new store with the connection pool
	store := db.NewStore(connPool)

	// Initialize token maker
	tokenMaker, err := token.NewPasetoMaker(config.TokenSecretKey)
	if err != nil {
		logger.Fatal("Cannot create token maker:", err)
	}

	// Initialize services
	aiSvc := services.NewAIService(config.OpenAIAPIKey, logger)
	authSvc := services.NewAuthService(store, tokenMaker, config, logger)
	researchSvc := services.NewResearchService(store, aiSvc, logger) // Pass logger

	// Setup Gin router and server
	server := api.NewServer(config, store, authSvc, researchSvc, aiSvc, tokenMaker, logger)

	// Start server
	srv := &http.Server{
		Addr:    ":" + config.Port,
		Handler: server.Router, // Assuming Router is a field in api.Server
	}

	// Graceful shutdown
	go func() {
		logger.Info("Server starting on port " + config.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("Failed to start server:", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown:", err)
	}

	logger.Info("Server exited")
}
