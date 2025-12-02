package messages

import (
	"encoding/json"
)

type MessageActionType string

const (
	MessageActionTypeJoin       MessageActionType = "join"
	MessageActionTypeLeave      MessageActionType = "leave"
	MessageActionTypeMessage    MessageActionType = "message"
	MessageActionTypeCreateRoom MessageActionType = "create_room"
	MessageActionTypePing       MessageActionType = "ping"
)

type EventType string

const (
	EventNewMessage EventType = "new_message"
	EventNewRoom    EventType = "new_room"
)

// WsMessage is the envelope for all WS messages
type WsMessage struct {
	Type    MessageActionType `json:"type"`
	Payload json.RawMessage   `json:"payload"`
}

// Payloads per message type

type JoinRoomPayload struct {
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
}

type LeaveRoomPayload struct {
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
}

type MessagePayload struct {
	Message string `json:"message"`
}

type CreateRoomPayload struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Message struct {
	Type        EventType      `json:"type"`
	RoomID      string         `json:"room_id"`
	UserID      string         `json:"room_name"`
	Message     MessagePayload `json:"message"`
	MessageTime string         `json:"message_time"` // ISO8601 string
}

type RoomCreate struct {
	Type     EventType `json:"type"`
	RoomID   string    `json:"room_id"`
	AuthorID string    `json:"author_id"`
	RoomName string    `json:"room_name"`
}

func NewMessage(RoomID string, userID string, message string, timestamp string) Message {
	return Message{
		Type:        EventNewMessage,
		RoomID:      RoomID,
		UserID:      userID,
		Message:     MessagePayload{Message: message},
		MessageTime: timestamp,
	}
}

func NewRoom(RoomID string, AuthorID string, name string) RoomCreate {
	return RoomCreate{
		Type:     EventNewRoom,
		RoomID:   RoomID,
		RoomName: name,
		AuthorID: AuthorID,
	}
}
