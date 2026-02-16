package server

import (
	"founders-toolkit-api/internal/auth"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func (s *Server) setupMiddlewares() {
	s.router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH", "*"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "*"},
		AllowCredentials: true,
	}))
}

func (s *Server) registerRoutes() {
	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authGroup := s.router.Group("/auth")
	{
		authGroup.POST("/signup", auth.SignUp(s.db))
		authGroup.POST("/login", auth.Login(s.db))
		authGroup.POST("/logout", auth.Logout)
		authGroup.POST("/refresh", auth.RefreshAccessToken(s.db))
		authGroup.POST("/change-password", auth.AuthenticateUser(s.db), auth.ChangePassword(s.db))
	}
}
