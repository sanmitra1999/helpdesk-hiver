package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"helpdesk/internal/app"
	"helpdesk/internal/db"

	_ "github.com/lib/pq"
)

// initializes the helpdesk application, connects to PostgreSQL database, and starts the HTTP server.
func main() {
	// Create a structured logger with timestamp and file location
	logger := log.New(os.Stdout, "helpdesk ", log.LstdFlags|log.LUTC|log.Lshortfile)

	port := getenv("PORT", "8080")
	dbHost := getenv("DB_HOST", "localhost")

	database, err := db.OpenDB(dbHost, "postgres", "password", "helpdesk", 5432)
	if err != nil {
		logger.Fatalf("failed to connect to database: %v", err)
	}
	defer database.Close()

	if err := db.InitSchema(database); err != nil {
		logger.Fatalf("failed to initialize database schema: %v", err)
	}

	// Create the application instance with database connection and logger
	application, err := app.New(database, logger)
	if err != nil {
		logger.Fatalf("failed to initialize app: %v", err)
	}

	// Configure HTTP server with timeout settings for security
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           application.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Printf("listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// getenv returns the environment variable value for key or a fallback value when unset.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
