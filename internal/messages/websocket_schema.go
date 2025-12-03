package messages

import (
	"encoding/json"
	"time"
)

type InputMessageActionType string

const (
	MessageActionTypeJoin       InputMessageActionType = "join"
	MessageActionTypeLeave      InputMessageActionType = "leave"
	MessageActionTypeMessage    InputMessageActionType = "message"
	MessageActionTypeCreateRoom InputMessageActionType = "create_room"
	MessageActionTypePing       InputMessageActionType = "ping"
)

type WsMessage struct {
	Type    InputMessageActionType `json:"type"`
	Payload json.RawMessage        `json:"payload"`
}

type JoinRoomPayload struct {
	RoomID   string `json:"room_id"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
}

type LeaveRoomPayload struct {
	RoomID string `json:"room_id"`
}

type MessagePayload struct {
	Message string `json:"message"`
}

type CreateRoomPayload struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
}

type EventType string

const (
	EventUserJoinedRoom EventType = "user_joined"
	EventUserLeftRoom   EventType = "user_left"
	EventNewMessage     EventType = "new_message"
	EventNewRoom        EventType = "new_room"
)

// WsMessage is the envelope for all WS messages

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type JoinSuccess struct {
	Type   string `json:"type"` // "join_success"
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
}

type Pong struct {
	Type string `json:"type"` // "pong"
}

type RoomMessageEvent struct {
	Type        EventType      `json:"type"`
	RoomID      string         `json:"room_id"`
	UserID      string         `json:"user_id"`
	UserName    string         `json:"user_name"`
	Message     MessagePayload `json:"message"`
	MessageTime string         `json:"message_time"` // ISO8601 string
}

type RoomCreateEvent struct {
	Type     EventType `json:"type"`
	RoomID   string    `json:"room_id"`
	AuthorID string    `json:"author_id"`
	RoomName string    `json:"room_name"`
}

type UserJoinedEvent struct {
	Type        EventType `json:"type"`
	RoomID      string    `json:"room_id"`
	UserID      string    `json:"user_id"`
	UserName    string    `json:"user_name"`
	MessageTime string    `json:"message_time"`
}

type UserLeftEvent struct {
	Type        EventType `json:"type"`
	RoomID      string    `json:"room_id"`
	UserID      string    `json:"user_id"`
	UserName    string    `json:"user_name"`
	MessageTime string    `json:"message_time"`
}

func NewRoomMessageEvent(roomID string, userID string, userName string, message string) RoomMessageEvent {
	return RoomMessageEvent{
		Type:        EventNewMessage,
		RoomID:      roomID,
		UserID:      userID,
		UserName:    userName,
		Message:     MessagePayload{Message: message},
		MessageTime: time.Now().UTC().Format(time.RFC3339),
	}
}

func NewUserJoinedEvent(roomID string, userID string, userName string) UserJoinedEvent {
	return UserJoinedEvent{
		Type:        EventUserJoinedRoom,
		RoomID:      roomID,
		UserID:      userID,
		UserName:    userName,
		MessageTime: time.Now().UTC().Format(time.RFC3339),
	}
}

func NewUserLeftEvent(roomID string, userID string, userName string) UserLeftEvent {
	return UserLeftEvent{
		Type:        EventUserLeftRoom,
		RoomID:      roomID,
		UserID:      userID,
		UserName:    userName,
		MessageTime: time.Now().UTC().Format(time.RFC3339),
	}
}

func NewRoom(RoomID string, AuthorID string, name string) RoomCreateEvent {
	return RoomCreateEvent{
		Type:     EventNewRoom,
		RoomID:   RoomID,
		RoomName: name,
		AuthorID: AuthorID,
	}
}

func NewJoinSuccess(roomID string, userID string) JoinSuccess {
	return JoinSuccess{
		Type:   "join_success",
		RoomID: roomID,
		UserID: userID,
	}
}
