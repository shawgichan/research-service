package api

import (
	"database/sql"
	"errors"

	"github.com/shawgichan/research-service/internal/api/response"
	apimodels "github.com/shawgichan/research-service/internal/models" // Alias to avoid clashes
	"github.com/shawgichan/research-service/internal/token"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Server) getCurrentUser(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)

	user, err := s.store.GetUserByID(c.Request.Context(), pgtype.UUID{Bytes: authPayload.UserID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("Current user not found in DB", "userID", authPayload.UserID)
			response.NotFound(c, "User not found")
			return
		}
		s.logger.Error("Failed to get current user from DB", "userID", authPayload.UserID, "error", err)
		response.InternalServerError(c, "Failed to retrieve user information", err)
		return
	}

	userResponse := apimodels.ToUserResponse(user)
	response.Ok(c, userResponse)
}
