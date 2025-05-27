package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/shawgichan/research-service/internal/api/response"
	"github.com/shawgichan/research-service/internal/token"

	"github.com/gin-gonic/gin"
)

const (
	authorizationHeaderKey  = "authorization"
	authorizationTypeBearer = "bearer"
	authorizationPayloadKey = "authorization_payload"
)

// authMiddleware creates a gin middleware for authorization
func authMiddleware(tokenMaker token.Maker) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorizationHeader := c.GetHeader(authorizationHeaderKey)
		if len(authorizationHeader) == 0 {
			response.Unauthorized(c, "authorization header is not provided")
			return
		}

		fields := strings.Fields(authorizationHeader)
		if len(fields) < 2 {
			response.Unauthorized(c, "invalid authorization header format")
			return
		}

		authType := strings.ToLower(fields[0])
		if authType != authorizationTypeBearer {
			response.Unauthorized(c, fmt.Sprintf("unsupported authorization type %s", authType))
			return
		}

		accessToken := fields[1]
		payload, err := tokenMaker.VerifyToken(accessToken)
		if err != nil {
			if errors.Is(err, token.ErrExpiredToken) {
				response.RespondError(c, http.StatusUnauthorized, "token has expired", "expired_token")
				return
			}
			response.Unauthorized(c, "invalid access token")
			return
		}

		c.Set(authorizationPayloadKey, payload)
		c.Next()
	}
}
