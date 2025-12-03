package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/arturskrzydlo/chat-room/internal/coordinator"
	"github.com/arturskrzydlo/chat-room/internal/messages"
	"github.com/gorilla/websocket"
)

type Client struct {
	rooms       map[string]struct{}
	userID      string
	userName    string
	conn        *websocket.Conn
	send        chan interface{}
	coordinator *coordinator.Coordinator
	ctx         context.Context
	cancel      context.CancelFunc
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
		c.send <- messages.Pong{Type: "pong"}

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

	c.rooms[p.RoomID] = struct{}{}

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

	c.rooms[p.RoomID] = struct{}{}

	log.Printf("User %s joined room: %s", c.userName, p.RoomID)

	c.send <- messages.NewJoinSuccess(p.RoomID, c.userID)
}

func (c *Client) handleLeaveRoom(msg *messages.WsMessage) {
	var p messages.LeaveRoomPayload
	if err := marshalPayload(msg.Payload, &p); err != nil {
		c.sendError("invalid_payload", err.Error())
		return
	}

	if _, ok := c.rooms[p.RoomID]; !ok {
		c.sendError("leave_room_error", "user not in this room")
		return
	}

	if err := c.coordinator.LeaveRoom(p.RoomID, c.userID); err != nil {
		c.sendError("leave_room_error", err.Error())
		return
	}

	log.Printf("User %s left room: %s", c.userID, p.RoomID)

	delete(c.rooms, p.RoomID)
}

func (c *Client) handleChatMessage(msg *messages.WsMessage) {
	var p messages.MessagePayload
	if err := marshalPayload(msg.Payload, &p); err != nil {
		c.sendError("invalid_payload", err.Error())
		return
	}

	if p.RoomID == "" {
		c.sendError("message_error", "room_id is required")
		return
	}

	if _, ok := c.rooms[p.RoomID]; !ok {
		c.sendError("message_error", "not in this room")
		return
	}

	if err := c.coordinator.SendMessage(p.RoomID, c.userID, p.Message); err != nil {
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

	// leave all joined rooms
	if c.userID != "" {
		for roomID := range c.rooms {
			err := c.coordinator.LeaveRoom(roomID, c.userID)
			if err != nil {
				log.Printf("couldn't leave room : %s err: %v", roomID, err)
			}
		}
	}
}

func marshalPayload(payload interface{}, target interface{}) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
