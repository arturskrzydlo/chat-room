package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/arturskrzydlo/chat-room/internal/messages"
	"github.com/arturskrzydlo/chat-room/internal/models"
)

type Coordinator struct {
	rooms *roomStore
}

func NewCoordinator() *Coordinator {
	return &Coordinator{
		rooms: newRoomStore(),
	}
}

func (c *Coordinator) CreateRoom(
	roomID string,
	authorID string,
	roomName string,
	send chan<- interface{},
) error {
	if roomID == "" || roomName == "" {
		return fmt.Errorf("room_id and room_name are required")
	}

	if _, exists := c.rooms.Load(roomID); exists {
		return fmt.Errorf("room with id %s already exists", roomID)
	}

	room := models.NewRoom(roomID, roomName, authorID)
	c.rooms.Store(roomID, room)

	go room.Run()

	// Auto-join author into the room
	authorUser := &models.User{ID: authorID, Name: authorID}
	roomClient := &models.RoomClient{
		UserID: authorID,
		User:   authorUser,
		Send:   send,
	}
	room.EnqueueJoin(roomClient)

	log.Printf("CreateRoom: roomID=%s author=%s", roomID, authorID)

	send <- messages.RoomCreate{
		Type:     messages.EventNewRoom,
		RoomID:   roomID,
		AuthorID: authorID,
		RoomName: roomName,
	}

	log.Printf("CreateRoom: sent new_room to author")

	return nil
}

func (c *Coordinator) GetRoom(roomID string) *models.Room {
	r, _ := c.rooms.Load(roomID)
	return r
}

func (c *Coordinator) JoinRoom(
	roomID string,
	userID string,
	userName string,
	send chan<- interface{},
) error {
	room := c.GetRoom(roomID)
	if room == nil {
		return fmt.Errorf("room %s not found", roomID)
	}

	if userID == "" || userName == "" {
		return fmt.Errorf("user_id and user_name are required")
	}

	users := room.GetUsers()
	if _, exists := users[userID]; exists {
		return fmt.Errorf("user %s already in room", userID)
	}

	user := &models.User{ID: userID, Name: userName}
	roomClient := &models.RoomClient{
		UserID: userID,
		User:   user,
		Send:   send,
	}

	// enqueue join
	room.EnqueueJoin(roomClient)

	// broadcast join system message
	joinMessage := messages.Message{
		Type:        messages.EventNewMessage,
		RoomID:      roomID,
		UserID:      "system",
		Message:     messages.MessagePayload{Message: fmt.Sprintf("%s joined the room", userName)},
		MessageTime: time.Now().UTC().Format(time.RFC3339),
	}
	room.EnqueueBroadcast(joinMessage)

	return nil
}

func (c *Coordinator) LeaveRoom(
	roomID string,
	userID string,
) error {
	room := c.GetRoom(roomID)
	if room == nil {
		return fmt.Errorf("room %s not found", roomID)
	}

	users := room.GetUsers()
	user, exists := users[userID]
	if !exists {
		return fmt.Errorf("user %s not in room %s", userID, roomID)
	}

	// enqueue leave
	room.EnqueueLeave(userID)

	// broadcast leave system message
	leaveMessage := messages.Message{
		Type:        messages.EventNewMessage,
		RoomID:      roomID,
		UserID:      "system",
		Message:     messages.MessagePayload{Message: fmt.Sprintf("%s left the room", user.Name)},
		MessageTime: time.Now().UTC().Format(time.RFC3339),
	}
	room.EnqueueBroadcast(leaveMessage)

	// possibly delete room
	c.deleteRoomIfEmpty(roomID)

	return nil
}

func (c *Coordinator) SendMessage(
	roomID string,
	userID string,
	content string,
) error {
	if content == "" {
		return fmt.Errorf("message content cannot be empty")
	}

	// 10KB limit
	if len(content) > 10*1024 {
		return fmt.Errorf("message exceeds 10KB limit")
	}

	room := c.GetRoom(roomID)
	if room == nil {
		return fmt.Errorf("room %s not found", roomID)
	}

	users := room.GetUsers()
	if _, exists := users[userID]; !exists {
		return fmt.Errorf("user %s not in room %s", userID, roomID)
	}

	msg := messages.Message{
		Type:        messages.EventNewMessage,
		RoomID:      roomID,
		UserID:      userID,
		Message:     messages.MessagePayload{Message: content},
		MessageTime: time.Now().UTC().Format(time.RFC3339),
	}

	room.EnqueueBroadcast(msg)
	return nil
}

func (c *Coordinator) deleteRoomIfEmpty(roomID string) {
	room := c.GetRoom(roomID)
	if room == nil {
		return
	}

	if room.GetUserCount() == 0 {
		c.rooms.Delete(roomID)
		room.EnqueueClose()
	}
}

func (c *Coordinator) Shutdown(ctx context.Context) error {
	rooms := make([]*models.Room, 0)
	for _, room := range c.rooms.rooms {
		rooms = append(rooms, room)
	}

	for _, room := range rooms {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			room.EnqueueClose()
		}
	}
	return nil
}
