package server

import (
	"fmt"
	"founders-toolkit-api/internal/bucket"
	"founders-toolkit-api/internal/database"
	"os"

	"github.com/gin-gonic/gin"
)

type Server struct {
	db     *database.Service
	bucket *bucket.Service
	router *gin.Engine
	port   string
}

func NewServer() *Server {
	db := database.New()
	router := gin.Default()
	// bucket := bucket.New()

	s := &Server{
		db: db,
		// bucket: bucket,
		router: router,
		port:   os.Getenv("PORT"),
	}

	s.setupMiddlewares()
	s.registerRoutes()

	return s
}

func (s *Server) Port() string {
	return s.port
}

func (s *Server) Bucket() *bucket.Service {
	return s.bucket
}

func (s *Server) Router() *gin.Engine {
	return s.router
}

func (s *Server) Run() error {
	addr := fmt.Sprintf(":%s", s.port)
	return s.router.Run(addr)
}
