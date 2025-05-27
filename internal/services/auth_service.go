package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shawgichan/research-service/internal/db"
	"github.com/shawgichan/research-service/internal/db/sqlc"
	applogger "github.com/shawgichan/research-service/internal/logger"
	"github.com/shawgichan/research-service/internal/models" // For response models
	"github.com/shawgichan/research-service/internal/token"
	"github.com/shawgichan/research-service/internal/util"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrUserAlreadyExists  = errors.New("user with this email already exists")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrSessionNotFound    = errors.New("session not found or expired")
	ErrSessionBlocked     = errors.New("session is blocked")
)

type AuthService struct {
	store      db.Store
	tokenMaker token.Maker
	config     util.Config
	logger     *applogger.AppLogger
}

func NewAuthService(store db.Store, tokenMaker token.Maker, config util.Config, logger *applogger.AppLogger) *AuthService {
	return &AuthService{
		store:      store,
		tokenMaker: tokenMaker,
		config:     config,
		logger:     logger,
	}
}

func (s *AuthService) Register(ctx context.Context, req models.RegisterUserRequest) (*models.LoginUserResponse, error) {
	s.logger.Info("Registering user", "email", req.Email)
	_, err := s.store.GetUserByEmail(ctx, req.Email)
	if err == nil {
		s.logger.Warn("User registration failed: email already exists", "email", req.Email)
		return nil, ErrUserAlreadyExists
	}
	if !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, sql.ErrNoRows) { // pgx.ErrNoRows for pgx direct, sql.ErrNoRows if using database/sql interface
		s.logger.Error("Failed to check existing user", "email", req.Email, "error", err)
		return nil, fmt.Errorf("database error checking user: %w", err)
	}

	hashedPassword, err := util.HashPassword(req.Password)
	if err != nil {
		s.logger.Error("Failed to hash password", "email", req.Email, "error", err)
		return nil, fmt.Errorf("could not hash password: %w", err)
	}

	createUserParams := sqlc.CreateUserParams{
		Email:        req.Email,
		PasswordHash: hashedPassword,
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		// IsVerified will default to FALSE in DB
	}

	user, err := s.store.CreateUser(ctx, createUserParams)
	if err != nil {
		s.logger.Error("Failed to create user in DB", "email", req.Email, "error", err)
		// Could check for unique constraint violation specifically
		return nil, fmt.Errorf("could not create user: %w", err)
	}

	s.logger.Info("User registered successfully", "userID", user.ID, "email", user.Email)
	// Consider sending a verification email here

	return s.createSessionAndTokens(ctx, user, "", "") // No user agent/IP for initial registration response
}

func (s *AuthService) Login(ctx context.Context, req models.LoginUserRequest, userAgent, clientIP string) (*models.LoginUserResponse, error) {
	s.logger.Info("User login attempt", "email", req.Email)
	user, err := s.store.GetUserByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("Login failed: user not found", "email", req.Email)
			return nil, ErrInvalidCredentials
		}
		s.logger.Error("Failed to get user by email", "email", req.Email, "error", err)
		return nil, fmt.Errorf("database error fetching user: %w", err)
	}

	err = util.CheckPassword(req.Password, user.PasswordHash)
	if err != nil {
		s.logger.Warn("Login failed: invalid password", "email", req.Email, "userID", user.ID)
		return nil, ErrInvalidCredentials
	}

	// Optional: Check if user is verified
	// if !user.IsVerified.Bool {
	//  s.logger.Warn("Login failed: user not verified", "email", req.Email, "userID", user.ID)
	// 	return nil, errors.New("user account is not verified")
	// }

	s.logger.Info("User login successful", "userID", user.ID, "email", user.Email)
	return s.createSessionAndTokens(ctx, user, userAgent, clientIP)
}

func (s *AuthService) createSessionAndTokens(ctx context.Context, user sqlc.User, userAgent, clientIP string) (*models.LoginUserResponse, error) {
	accessToken, accessPayload, err := s.tokenMaker.CreateToken(user.ID.Bytes, s.config.AccessTokenDuration)
	if err != nil {
		s.logger.Error("Failed to create access token", "userID", user.ID, "error", err)
		return nil, fmt.Errorf("could not create access token: %w", err)
	}

	refreshToken, refreshPayload, err := s.tokenMaker.CreateToken(user.ID.Bytes, s.config.RefreshTokenDuration)
	if err != nil {
		s.logger.Error("Failed to create refresh token", "userID", user.ID, "error", err)
		return nil, fmt.Errorf("could not create refresh token: %w", err)
	}

	sessionParams := sqlc.CreateSessionParams{
		ID:           pgtype.UUID{Bytes: refreshPayload.ID, Valid: true}, // Use Paseto payload ID as session ID
		UserID:       user.ID,
		RefreshToken: refreshToken,
		UserAgent:    pgtype.Text{String: userAgent, Valid: userAgent != ""},
		ClientIp:     pgtype.Text{String: clientIP, Valid: clientIP != ""},
		IsBlocked:    pgtype.Bool{Bool: false, Valid: true},
		ExpiresAt:    pgtype.Timestamptz{Time: refreshPayload.ExpiredAt, Valid: true},
	}
	session, err := s.store.CreateSession(ctx, sessionParams)
	if err != nil {
		s.logger.Error("Failed to create session", "userID", user.ID, "error", err)
		return nil, fmt.Errorf("could not create session: %w", err)
	}

	loginResponse := &models.LoginUserResponse{
		SessionID:             session.ID.Bytes,
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessPayload.ExpiredAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshPayload.ExpiredAt,
		User:                  models.ToUserResponse(user),
	}
	return loginResponse, nil
}

func (s *AuthService) RefreshAccessToken(ctx context.Context, refreshToken string, userAgent, clientIP string) (*models.LoginUserResponse, error) {
	s.logger.Info("Attempting to refresh access token")
	refreshPayload, err := s.tokenMaker.VerifyToken(refreshToken)
	if err != nil {
		s.logger.Warn("Refresh token verification failed", "error", err)
		return nil, token.ErrInvalidToken // Use token.ErrInvalidToken or token.ErrExpiredToken
	}

	session, err := s.store.GetSessionByRefreshToken(ctx, refreshToken) // Query should use refresh_token as string
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("Session not found for refresh token", "token_id", refreshPayload.ID)
			return nil, ErrSessionNotFound
		}
		s.logger.Error("Failed to get session by refresh token", "token_id", refreshPayload.ID, "error", err)
		return nil, fmt.Errorf("database error fetching session: %w", err)
	}

	if session.IsBlocked.Bool {
		s.logger.Warn("Session is blocked", "session_id", session.ID, "userID", session.UserID)
		return nil, ErrSessionBlocked
	}

	if session.UserID.Bytes != refreshPayload.UserID {
		s.logger.Warn("Mismatched user ID in session and token", "session_userID", session.UserID, "token_userID", refreshPayload.UserID)
		return nil, ErrSessionNotFound // Or a more specific error
	}

	if time.Now().After(session.ExpiresAt.Time) {
		s.logger.Warn("Refresh token / session has expired", "session_id", session.ID, "expires_at", session.ExpiresAt.Time)
		return nil, ErrSessionNotFound // Or token.ErrExpiredToken
	}

	// (Optional but good practice) Refresh token rotation:
	// Block the current session, create a new refresh token and session.
	// This helps mitigate replay attacks if a refresh token is compromised.
	// For simplicity, this example reuses the existing refresh token if it's still valid.
	// If implementing rotation, make sure to delete/invalidate the old session.

	user, err := s.store.GetUserByID(ctx, pgtype.UUID{Bytes: refreshPayload.UserID, Valid: true})
	if err != nil {
		s.logger.Error("Failed to get user by ID during token refresh", "userID", refreshPayload.UserID, "error", err)
		return nil, fmt.Errorf("could not retrieve user: %w", err)
	}

	s.logger.Info("Access token refreshed successfully", "userID", user.ID)
	// Recreate only access token, or full new session if rotating refresh tokens
	accessToken, accessPayload, err := s.tokenMaker.CreateToken(user.ID.Bytes, s.config.AccessTokenDuration)
	if err != nil {
		s.logger.Error("Failed to create new access token during refresh", "userID", user.ID, "error", err)
		return nil, fmt.Errorf("could not create access token: %w", err)
	}

	// If not rotating refresh tokens, response uses existing refresh token details
	loginResponse := &models.LoginUserResponse{
		SessionID:             session.ID.Bytes,
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessPayload.ExpiredAt,
		RefreshToken:          refreshToken, // The same refresh token
		RefreshTokenExpiresAt: session.ExpiresAt.Time,
		User:                  models.ToUserResponse(user),
	}
	return loginResponse, nil
}

func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	s.logger.Info("User logout attempt")
	_, err := s.tokenMaker.VerifyToken(refreshToken)
	if err != nil {
		s.logger.Warn("Invalid refresh token provided for logout", "error", err)
		return token.ErrInvalidToken
	}

	// Instead of deleting, mark the session as blocked or just delete it.
	// Deleting is simpler for this example.
	err = s.store.DeleteSessionByRefreshToken(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			s.logger.Info("Session for refresh token already deleted or not found", "error", err)
			return nil // Idempotent: already logged out
		}
		s.logger.Error("Failed to delete session on logout", "error", err)
		return fmt.Errorf("could not delete session: %w", err)
	}
	s.logger.Info("User logged out successfully")
	return nil
}
