package auth

import (
	"founders-toolkit-api/internal/database"
	"founders-toolkit-api/internal/response"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	ErrAuthHeaderMissing = "Authorization header missing"
	ErrTokenMissing      = "Token missing or invalid"
)

func abort(c *gin.Context, msg string) {
	response.Respond(c, http.StatusUnauthorized, msg, nil)
	c.Abort()
}

func AuthenticateUser(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		const prefix = "Bearer "

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, prefix) {
			abort(c, ErrAuthHeaderMissing)
			return
		}

		tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))

		if tokenString == "" {
			abort(c, ErrTokenMissing)
			return
		}

		claims, err := ParseToken(tokenString)
		if err != nil {
			abort(c, err.Error())
			return
		}

		user, err := db.FindUserById(claims.Subject)
		if err != nil || user.ID == 0 {
			response.Respond(c, http.StatusNotFound, ErrUserNotFound, nil)
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Next()
	}
}
