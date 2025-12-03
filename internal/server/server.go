package server

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/arturskrzydlo/chat-room/internal/coordinator"
	"github.com/gorilla/websocket"
)

const (
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	writeWait      = 10 * time.Second
	maxMessageSize = 10 * 1024 // 10KB
)

type WsServer struct {
	coordinator *coordinator.Coordinator
	upgrader    websocket.Upgrader

	ctx        context.Context
	cancel     context.CancelFunc
	clients    map[*Client]struct{}
	clientDone chan *Client
}

func NewWsServer(ctx context.Context, coordinator *coordinator.Coordinator) *WsServer {
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

	go client.writePump()
	client.readPump()
	s.clientDone <- client
}

func (s *WsServer) watchClients() {
	for {
		select {
		case c := <-s.clientDone:
			delete(s.clients, c)
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *WsServer) Shutdown(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}

	clients := make([]*Client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}

	for _, c := range clients {
		if c.cancel != nil {
			c.cancel()
		}
		_ = c.conn.Close()
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if len(s.clients) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
