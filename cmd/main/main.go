package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arturskrzydlo/chat-room/internal/app"
	"github.com/arturskrzydlo/chat-room/internal/server"
)

func main() {
	// Create coordinator
	coordinator := app.NewCoordinator()

	// Create WebSocket server
	wsServer := server.NewWsServer(coordinator)

	// Setup HTTP routes
	http.Handle("/ws", wsServer)

	// Optional: Add health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Server configuration
	srv := &http.Server{
		Addr:           ":8080",
		Handler:        http.DefaultServeMux,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	// Graceful shutdown handling
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Shutdown all rooms
		if err := coordinator.Shutdown(ctx); err != nil {
			log.Printf("Coordinator shutdown error: %v", err)
		}

		// Shutdown HTTP server
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Println("Chat room server started at http://localhost:8080")
	log.Println("WebSocket endpoint: ws://localhost:8080/ws")

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
