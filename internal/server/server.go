package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/arturskrzydlo/chat-room/internal/app"
	"github.com/arturskrzydlo/chat-room/internal/messages"
	"github.com/gorilla/websocket"
)

const (
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	writeWait      = 10 * time.Second
	maxMessageSize = 10 * 1024 // 10KB
)

type WsServer struct {
	coordinator *app.Coordinator
	upgrader    websocket.Upgrader
}

type Client struct {
	roomID      string
	userID      string
	userName    string
	conn        *websocket.Conn
	send        chan interface{}
	coordinator *app.Coordinator
	ctx         context.Context
	cancel      context.CancelFunc
}

func NewWsServer(coordinator *app.Coordinator) *WsServer {
	return &WsServer{
		coordinator: coordinator,
		upgrader: websocket.Upgrader{
			CheckOrigin:     func(r *http.Request) bool { return true },
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
}

func (s *WsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	client := &Client{
		conn:        conn,
		send:        make(chan interface{}, 32), // buffered for concurrency
		coordinator: s.coordinator,
		ctx:         ctx,
		cancel:      cancel,
	}

	go client.writePump()
	client.readPump()
}

func (c *Client) readPump() {
	defer c.cleanup()

	c.setupReadTimeouts()

	for {
		msg, err := c.readMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket unexpected close during read: %v", err)
			}
			break
		}

		c.dispatchMessage(msg)
	}
}

func (c *Client) readMessage() (*messages.WsMessage, error) {
	_, rawMsg, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	// Check message size
	if len(rawMsg) > maxMessageSize {
		c.sendError("message_too_large", "message exceeds 10KB limit")
		return nil, fmt.Errorf("message too large")
	}

	var msg messages.WsMessage
	err = json.Unmarshal(rawMsg, &msg)
	if err != nil {
		c.sendError("malformed_json", "invalid JSON message")
		return nil, fmt.Errorf("malformed json message")
	}

	return &msg, nil
}

func (c *Client) setupReadTimeouts() {
	c.conn.SetReadLimit(maxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Printf("readPump: SetReadDeadline error: %v", err)
		return
	}

	c.conn.SetPongHandler(func(string) error {
		if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			log.Printf("readPump: pong handler deadline error: %v", err)
			return err
		}
		return nil
	})
}

func (c *Client) dispatchMessage(msg *messages.WsMessage) {
	switch msg.Type {
	case messages.MessageActionTypeCreateRoom:
		c.handleCreateRoom(msg)

	case messages.MessageActionTypeJoin:
		c.handleJoinRoom(msg)

	case messages.MessageActionTypeLeave:
		c.handleLeaveRoom(msg)

	case messages.MessageActionTypeMessage:
		c.handleChatMessage(msg)

	case messages.MessageActionTypePing:
		c.send <- map[string]string{"type": "pong"}

	default:
		c.sendError("invalid_message_type", fmt.Sprintf("unknown message type: %s", msg.Type))
	}
}

func (c *Client) handleCreateRoom(msg *messages.WsMessage) {
	var p messages.CreateRoomPayload
	if err := marshalPayload(msg.Payload, &p); err != nil {
		c.sendError("invalid_payload", err.Error())
		return
	}

	// allow create_room to establish identity once
	if p.UserID != "" || p.UserName != "" {
		if err := c.ensureIdentity(p.UserID, p.UserName); err != nil {
			c.sendError("identity_error", err.Error())
			return
		}
	}
	if c.userID == "" {
		c.sendError("identity_error", "user not identified yet")
		return
	}

	c.roomID = p.RoomID

	if err := c.coordinator.CreateRoom(p.RoomID, c.userID, p.RoomName, c.send); err != nil {
		c.sendError("create_room_error", err.Error())
		return
	}
}

func (c *Client) handleJoinRoom(msg *messages.WsMessage) {
	var p messages.JoinRoomPayload
	if err := marshalPayload(msg.Payload, &p); err != nil {
		c.sendError("invalid_payload", err.Error())
		return
	}

	userID := p.UserID
	userName := p.UserName
	if userID == "" {
		userID = c.userID
	}
	if userName == "" {
		userName = c.userName
	}

	if err := c.ensureIdentity(userID, userName); err != nil {
		c.sendError("identity_error", err.Error())
		return
	}

	if err := c.coordinator.JoinRoom(p.RoomID, c.userID, c.userName, c.send); err != nil {
		c.sendError("join_room_error", err.Error())
		return
	}

	c.roomID = p.RoomID

	log.Printf("User %s joined room: %s", c.userName, p.RoomID)

	c.send <- map[string]interface{}{
		"type":    "join_success",
		"room_id": p.RoomID,
		"user_id": c.userID,
	}
}

func (c *Client) handleLeaveRoom(msg *messages.WsMessage) {
	var p messages.LeaveRoomPayload
	if err := marshalPayload(msg.Payload, &p); err != nil {
		c.sendError("invalid_payload", err.Error())
		return
	}

	if p.RoomID != c.roomID {
		c.sendError("leave_room_error", "user not in this room")
		return
	}

	if err := c.coordinator.LeaveRoom(p.RoomID, c.userID); err != nil {
		c.sendError("leave_room_error", err.Error())
		return
	}

	log.Printf("User %s left room: %s", c.userID, p.RoomID)

	c.roomID = ""
	c.userID = ""
	c.userName = ""

}

func (c *Client) handleChatMessage(msg *messages.WsMessage) {
	if c.roomID == "" {
		c.sendError("message_error", "not in a room")
		return
	}

	var p messages.MessagePayload
	if err := marshalPayload(msg.Payload, &p); err != nil {
		c.sendError("invalid_payload", err.Error())
		return
	}

	if err := c.coordinator.SendMessage(c.roomID, c.userID, p.Message); err != nil {
		c.sendError("message_error", err.Error())
		return
	}
}

func (c *Client) sendError(code, message string) {
	c.send <- messages.ErrorPayload{
		Code:    code,
		Message: message,
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		if err := c.conn.Close(); err != nil {
			log.Printf("writePump: error closing connection: %v", err)
		}
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Printf("writePump: SetWriteDeadline error: %v", err)
				return
			}

			if !ok {
				// channel closed
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					log.Printf("writePump: WriteMessage close error: %v", err)
				}
				return
			}

			if err := c.conn.WriteJSON(msg); err != nil {
				log.Printf("writePump: WriteJSON error: %v", err)
				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Printf("writePump: SetWriteDeadline error: %v", err)
				return
			}

			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("writePump: WriteMessage ping error: %v", err)
				return
			}

		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Client) ensureIdentity(userID, userName string) error {
	if c.userID == "" {
		c.userID = userID
		c.userName = userName
		return nil
	}
	if c.userID != userID {
		return fmt.Errorf("connection already bound to user %s", c.userID)
	}
	return nil
}

func (c *Client) cleanup() {
	if c.cancel != nil {
		c.cancel()
	}

	if c.roomID != "" && c.userID != "" {
		_ = c.coordinator.LeaveRoom(c.roomID, c.userID)
	}
}

func marshalPayload(payload interface{}, target interface{}) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
