package main

import (
	"founders-toolkit-api/internal/server"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	if os.Getenv("APP_ENV") == "development" {
		err := godotenv.Load()
		if err != nil {
			log.Fatalf("err loading: %v", err)
		}

		fmt.Println("APP_ENV =", os.Getenv("OPENAI_API_KEY"))
	}
	s := server.NewServer()
	srv := &http.Server{
		Addr:         "127.0.0.1:" + s.Port(),
		Handler:      s.Router(),
		ReadTimeout:  180 * time.Second,
		WriteTimeout: 180 * time.Second,
		IdleTimeout:  time.Minute,
	}

	if os.Getenv("APP_ENV") != "development" {
		srv.Addr = "0.0.0.0:" + s.Port()
	}

	done := make(chan struct{})

	go gracefulShutdown(srv, done)

	log.Printf("Starting server on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %s", err)
	}

	<-done
	log.Println("Graceful shutdown complete.")
}

func gracefulShutdown(apiServer *http.Server, done chan struct{}) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	log.Println("shutting down gracefully, press Ctrl+C again to force")
	stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown with error: %v", err)
	}

	log.Println("Server exiting")

	close(done)
}
