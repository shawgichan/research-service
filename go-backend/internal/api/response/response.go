package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
	// Alias to avoid clash if any
)

func RespondJSON(c *gin.Context, statusCode int, payload interface{}) {
	c.JSON(statusCode, payload)
}

func RespondError(c *gin.Context, statusCode int, message string, details ...interface{}) {
	errPayload := gin.H{"error": message}
	if len(details) > 0 {
		errPayload["details"] = details
	}
	c.AbortWithStatusJSON(statusCode, errPayload)
}

func RespondSuccess(c *gin.Context, statusCode int, data interface{}, message ...string) {
	payload := gin.H{"success": true}
	if data != nil {
		payload["data"] = data
	}
	if len(message) > 0 && message[0] != "" {
		payload["message"] = message[0]
	}
	RespondJSON(c, statusCode, payload)
}

// Specific error responses
func BadRequest(c *gin.Context, message string, details ...interface{}) {
	RespondError(c, http.StatusBadRequest, message, details...)
}

func Unauthorized(c *gin.Context, message string) {
	RespondError(c, http.StatusUnauthorized, message)
}

func Forbidden(c *gin.Context, message string) {
	RespondError(c, http.StatusForbidden, message)
}

func NotFound(c *gin.Context, message string) {
	RespondError(c, http.StatusNotFound, message)
}

func InternalServerError(c *gin.Context, message string, err error) {
	// Log the internal error
	if err != nil {
		c.Error(err) // Gin will handle logging this if middleware is set up
	}
	RespondError(c, http.StatusInternalServerError, message)
}

// Specific success responses
func Ok(c *gin.Context, data interface{}, message ...string) {
	RespondSuccess(c, http.StatusOK, data, message...)
}

func Created(c *gin.Context, data interface{}, message ...string) {
	RespondSuccess(c, http.StatusCreated, data, message...)
}

func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
