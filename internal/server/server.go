package server

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	writeWait      = 10 * time.Second
	maxMessageSize = 10 * 1024 // 10KB
)

type WsServer struct {
	coordinator CoordinatorPort
	upgrader    websocket.Upgrader

	ctx        context.Context
	cancel     context.CancelFunc
	clientsMu  sync.RWMutex
	clients    map[*Client]struct{}
	clientDone chan *Client
}

func NewWsServer(ctx context.Context, coordinator CoordinatorPort) *WsServer {
	ctx, cancel := context.WithCancel(ctx)

	s := &WsServer{
		coordinator: coordinator,
		upgrader: websocket.Upgrader{
			CheckOrigin:     func(r *http.Request) bool { return true },
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		ctx:        ctx,
		cancel:     cancel,
		clients:    make(map[*Client]struct{}),
		clientDone: make(chan *Client, 128),
	}

	go s.watchClients()

	return s
}

func (s *WsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	client := &Client{
		rooms:       make(map[string]struct{}),
		conn:        conn,
		send:        make(chan interface{}, 32), // buffered for concurrency
		coordinator: s.coordinator,
		ctx:         ctx,
		cancel:      cancel,
	}

	s.clientsMu.Lock()
	s.clients[client] = struct{}{}
	s.clientsMu.Unlock()

	go client.writePump()

	func() {
		defer func() {
			client.cleanup()
			s.clientDone <- client
		}()
		client.readPump()
	}()
}

func (s *WsServer) watchClients() {
	for {
		select {
		case c := <-s.clientDone:
			s.clientsMu.Lock()
			delete(s.clients, c)
			s.clientsMu.Unlock()
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *WsServer) Shutdown(ctx context.Context) error {
	defer func() {
		if s.cancel != nil {
			s.cancel()
		}
	}()

	s.clientsMu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clientsMu.Unlock()

	for _, c := range clients {
		if c.cancel != nil {
			c.cancel()
		}
		_ = c.conn.Close()
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.clientsMu.RLock()
		count := len(s.clients)
		s.clientsMu.RUnlock()

		if count == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
