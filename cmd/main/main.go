package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arturskrzydlo/chat-room/internal/coordinator"
	"github.com/arturskrzydlo/chat-room/internal/server"
)

const (
	serverAddr            = ":8080"
	serverReadTimeout     = 15 * time.Second
	serverWriteTimeout    = 15 * time.Second
	serverShutdownTimeout = 30 * time.Second
	serverMaxHeaderBytes  = 1 * 1024 * 1024 // 1MB
)

func main() {
	coord := coordinator.NewCoordinator()
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	wsServer := server.NewWsServer(rootCtx, coord)

	http.Handle("/ws", wsServer)

	// Optional: Add health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})

	srv := &http.Server{
		Addr:           serverAddr,
		Handler:        http.DefaultServeMux,
		ReadTimeout:    serverReadTimeout,
		WriteTimeout:   serverWriteTimeout,
		MaxHeaderBytes: serverMaxHeaderBytes,
	}

	// Graceful shutdown handling
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()

		// Stop accepting new WS and close all clients
		if err := wsServer.Shutdown(ctx); err != nil {
			log.Printf("WebSocket server shutdown error: %v", err)
		}

		// Shutdown all rooms
		if err := coord.Shutdown(ctx); err != nil {
			log.Printf("Coordinator shutdown error: %v", err)
		}

		// Shutdown HTTP server
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("Chat room server started at http://localhost%s\n", serverAddr)
	log.Printf("WebSocket endpoint: ws://localhost%s/ws\n", serverAddr)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server error: %v", err)
	}
}
