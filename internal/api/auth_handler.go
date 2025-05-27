package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/shawgichan/research-service/internal/api/response"
	"github.com/shawgichan/research-service/internal/models" // API request/response models
	"github.com/shawgichan/research-service/internal/services"
	"github.com/shawgichan/research-service/internal/token" // For authorizationPayloadKey

	"github.com/gin-gonic/gin"
)

func (s *Server) registerUser(c *gin.Context) {
	var req models.RegisterUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("Invalid registration request", "error", err)
		response.BadRequest(c, "Invalid request payload", err.Error())
		return
	}

	loginResp, err := s.authService.Register(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, services.ErrUserAlreadyExists) {
			s.logger.Info("Registration attempt for existing email", "email", req.Email)
			response.RespondError(c, http.StatusConflict, services.ErrUserAlreadyExists.Error())
			return
		}
		s.logger.Error("User registration service error", "email", req.Email, "error", err)
		response.InternalServerError(c, "Failed to register user", err)
		return
	}

	// Set cookies for tokens (optional, but common for web apps)
	// s.setAuthCookies(c, loginResp.AccessToken, loginResp.RefreshToken, loginResp.AccessTokenExpiresAt, loginResp.RefreshTokenExpiresAt)

	response.Created(c, loginResp, "User registered successfully")
}

func (s *Server) loginUser(c *gin.Context) {
	var req models.LoginUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("Invalid login request", "error", err)
		response.BadRequest(c, "Invalid request payload", err.Error())
		return
	}

	userAgent := c.Request.UserAgent()
	clientIP := c.ClientIP()

	loginResp, err := s.authService.Login(c.Request.Context(), req, userAgent, clientIP)
	if err != nil {
		if errors.Is(err, services.ErrInvalidCredentials) {
			s.logger.Warn("Invalid login attempt", "email", req.Email)
			response.Unauthorized(c, services.ErrInvalidCredentials.Error())
			return
		}
		s.logger.Error("User login service error", "email", req.Email, "error", err)
		response.InternalServerError(c, "Failed to log in", err)
		return
	}

	// s.setAuthCookies(c, loginResp.AccessToken, loginResp.RefreshToken, loginResp.AccessTokenExpiresAt, loginResp.RefreshTokenExpiresAt)
	response.Ok(c, loginResp, "Login successful")
}

func (s *Server) refreshToken(c *gin.Context) {
	var req models.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("Invalid refresh token request", "error", err)
		response.BadRequest(c, "Invalid request payload", err.Error())
		return
	}

	userAgent := c.Request.UserAgent()
	clientIP := c.ClientIP()

	loginResp, err := s.authService.RefreshAccessToken(c.Request.Context(), req.RefreshToken, userAgent, clientIP)
	if err != nil {
		if errors.Is(err, token.ErrInvalidToken) || errors.Is(err, token.ErrExpiredToken) || errors.Is(err, services.ErrSessionNotFound) || errors.Is(err, services.ErrSessionBlocked) {
			s.logger.Warn("Refresh token processing failed", "error", err)
			response.Unauthorized(c, "Invalid or expired refresh token")
			return
		}
		s.logger.Error("Refresh token service error", "error", err)
		response.InternalServerError(c, "Failed to refresh token", err)
		return
	}

	// s.setAuthCookies(c, loginResp.AccessToken, loginResp.RefreshToken, loginResp.AccessTokenExpiresAt, loginResp.RefreshTokenExpiresAt)
	response.Ok(c, loginResp, "Token refreshed successfully")
}

func (s *Server) logoutUser(c *gin.Context) {
	var req models.RefreshTokenRequest // Assuming logout uses refresh token to invalidate session
	// Or, if you use access token to identify session from payload:
	// authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	// // And your service method `authService.Logout(ctx, authPayload.ID)` uses sessionID

	if err := c.ShouldBindJSON(&req); err != nil {
		// If logout doesn't need a body (e.g., invalidates based on Bearer token's session claim)
		// then this part might change. For session invalidation via refresh token:
		s.logger.Warn("Invalid logout request, refresh_token expected in body", "error", err)
		response.BadRequest(c, "refresh_token is required for logout")
		return
	}

	err := s.authService.Logout(c.Request.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, token.ErrInvalidToken) { // If service returns this for bad refresh token
			response.Unauthorized(c, "Invalid refresh token for logout")
			return
		}
		s.logger.Error("Logout service error", "error", err)
		response.InternalServerError(c, "Failed to logout", err)
		return
	}

	// s.clearAuthCookies(c)
	response.Ok(c, nil, "Logout successful")
}

// Helper for setting cookies (optional)
func (s *Server) setAuthCookies(c *gin.Context, accessToken, refreshToken string, accessExp, refreshExp time.Time) {
	httpOnly := true
	secure := s.config.Environment != "development" // Use secure cookies in prod

	// Access Token Cookie
	accessMaxAge := int(time.Until(accessExp).Seconds())
	c.SetCookie("access_token", accessToken, accessMaxAge, "/", "", secure, httpOnly)

	// Refresh Token Cookie
	refreshMaxAge := int(time.Until(refreshExp).Seconds())
	c.SetCookie("refresh_token", refreshToken, refreshMaxAge, "/api/v1/auth/refresh-token", "", secure, httpOnly) // Path specific to refresh
}

// Helper for clearing cookies (optional)
func (s *Server) clearAuthCookies(c *gin.Context) {
	c.SetCookie("access_token", "", -1, "/", "", false, true)
	c.SetCookie("refresh_token", "", -1, "/api/v1/auth/refresh-token", "", false, true)
}
