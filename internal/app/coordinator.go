package app

import (
	"context"
	"fmt"
	"log"

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

	send <- messages.NewRoom(roomID, authorID, roomName)

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

	room.EnqueueJoin(roomClient)
	joinMessage := messages.NewUserJoinedEvent(roomID, userID, userName)
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

	room.EnqueueLeave(userID)
	leaveMessage := messages.NewUserLeftEvent(roomID, userID, user.Name)
	room.EnqueueBroadcast(leaveMessage)

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
	user, exists := users[userID]
	if !exists {
		return fmt.Errorf("user %s not in room %s", userID, roomID)
	}

	msg := messages.NewRoomMessageEvent(roomID, userID, user.Name, content)
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
