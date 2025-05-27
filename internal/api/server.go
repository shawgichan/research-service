package api

import (
	"time"

	"github.com/shawgichan/research-service/internal/db"
	applogger "github.com/shawgichan/research-service/internal/logger"
	"github.com/shawgichan/research-service/internal/services"
	"github.com/shawgichan/research-service/internal/token"
	"github.com/shawgichan/research-service/internal/util"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type Server struct {
	config          util.Config
	store           db.Store
	authService     *services.AuthService
	researchService *services.ResearchService
	aiService       *services.AIService
	tokenMaker      token.Maker
	logger          *applogger.AppLogger
	Router          *gin.Engine
}

func NewServer(
	config util.Config,
	store db.Store,
	authService *services.AuthService,
	researchService *services.ResearchService,
	aiService *services.AIService,
	tokenMaker token.Maker,
	logger *applogger.AppLogger,
) *Server {
	server := &Server{
		config:          config,
		store:           store,
		authService:     authService,
		researchService: researchService,
		aiService:       aiService,
		tokenMaker:      tokenMaker,
		logger:          logger,
	}

	router := gin.New() // Use gin.New() for more control over middleware

	// Global Middleware
	router.Use(gin.Recovery()) // Recover from any panics
	// Custom logger middleware can be added here if Gin's default is not sufficient
	router.Use(CORSMiddleware()) // CORS

	server.Router = router
	server.setupRoutes()
	return server
}

func (s *Server) setupRoutes() {}

// CORSMiddleware sets up Cross-Origin Resource Sharing
func CORSMiddleware() gin.HandlerFunc {
	return cors.New(cors.Config{
		// AllowOrigins:     []string{"http://localhost:3000", "https://your-frontend-domain.com"},
		AllowAllOrigins:  true, // For development; be more restrictive in production
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Accept"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
