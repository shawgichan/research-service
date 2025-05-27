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

func (s *Server) setupRoutes() {
	router := s.Router

	// Health check
	router.GET("/health", s.healthCheckHandler)

	v1 := router.Group("/api/v1")

	// Authentication routes
	authRoutes := v1.Group("/auth")
	{
		authRoutes.POST("/register", s.registerUser)
		authRoutes.POST("/login", s.loginUser)
		authRoutes.POST("/refresh-token", s.refreshToken)
		// Logout needs to be authenticated to identify the session to invalidate
		// authRoutes.POST("/logout", authMiddleware(s.tokenMaker), s.logoutUser)
	}

	// Authenticated routes
	authRequired := v1.Group("/").Use(authMiddleware(s.tokenMaker))

	// Logout (needs to be authenticated to know which session to end)
	authRequired.POST("/auth/logout", s.logoutUser)

	// User routes
	userRoutes := v1.Group("/users").Use(authMiddleware(s.tokenMaker))
	{
		userRoutes.GET("/me", s.getCurrentUser)
	}

	// Project routes
	projectRoutes := v1.Group("/projects").Use(authMiddleware(s.tokenMaker))
	{
		projectRoutes.POST("", s.createProject)
		projectRoutes.GET("", s.listUserProjects)
		projectRoutes.GET("/:project_id", s.getProject)
		projectRoutes.PUT("/:project_id", s.updateProject)
		projectRoutes.DELETE("/:project_id", s.deleteProject)

		// Nested Chapter routes under projects
		projectRoutes.POST("/:project_id/chapters", s.createChapter)
		projectRoutes.GET("/:project_id/chapters", s.listProjectChapters)
		projectRoutes.PUT("/:project_id/chapters/:chapter_id", s.updateChapter)
		projectRoutes.POST("/:project_id/chapters/:chapter_id/generate-content", s.generateChapterContentHandler)
		// DELETE chapter: projectRoutes.DELETE("/:project_id/chapters/:chapter_id", s.deleteChapter)

		// Nested Reference routes under projects
		projectRoutes.POST("/:project_id/references", s.createReference)
		projectRoutes.GET("/:project_id/references", s.listProjectReferences)
		projectRoutes.DELETE("/:project_id/references/:reference_id", s.deleteReference)

		// Nested Document routes
		projectRoutes.POST("/:project_id/documents/generate", s.generateDocumentHandler)
		projectRoutes.GET("/:project_id/documents/:document_id/download", s.downloadDocumentHandler) // This would need file serving
	}
}

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

func (s *Server) healthCheckHandler(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}
