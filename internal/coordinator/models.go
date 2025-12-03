package coordinator

import (
	"sync"
	"time"
)

type User struct {
	ID   string
	Name string
}

type roomEventType int

const (
	roomEventJoin roomEventType = iota
	roomEventLeave
	roomEventBroadcast
	roomEventClose
)

type roomEvent struct {
	kind   roomEventType
	client *RoomClient
	userID string
	msg    interface{}
}

// Room represents a chat room with multiple users
type Room struct {
	ID        string
	Name      string
	AuthorID  string
	CreatedAt time.Time

	mu      sync.RWMutex
	users   map[string]*User              // userID -> User
	clients map[string]chan<- interface{} // userID -> send channel

	events chan roomEvent
}

// RoomClient wraps client info for joining a room
type RoomClient struct {
	UserID string
	User   *User
	Send   chan<- interface{}
}

func NewRoom(id, name, authorID string) *Room {
	room := &Room{
		ID:        id,
		Name:      name,
		AuthorID:  authorID,
		CreatedAt: time.Now().UTC(),
		users:     make(map[string]*User),
		clients:   make(map[string]chan<- interface{}),
		events:    make(chan roomEvent, 128), // buffered to prevent blocking
	}
	return room
}

// Run starts the room's main event loop
func (r *Room) Run() {
	defer r.cleanup()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				return
			}
			switch ev.kind {
			case roomEventJoin:
				r.handleJoin(ev.client)
			case roomEventLeave:
				r.handleLeave(ev.userID)
			case roomEventBroadcast:
				r.handleBroadcast(ev.msg)
			case roomEventClose:
				return
			}
		case <-ticker.C:
			// Periodic cleanup or health checks can go here
		}
	}
}

func (r *Room) EnqueueJoin(c *RoomClient) {
	r.events <- roomEvent{kind: roomEventJoin, client: c}
}

func (r *Room) EnqueueLeave(userID string) {
	r.events <- roomEvent{kind: roomEventLeave, userID: userID}
}

func (r *Room) EnqueueBroadcast(msg interface{}) {
	r.events <- roomEvent{kind: roomEventBroadcast, msg: msg}
}

func (r *Room) EnqueueClose() {
	r.events <- roomEvent{kind: roomEventClose}
}

func (r *Room) handleJoin(client *RoomClient) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.users[client.UserID] = client.User
	r.clients[client.UserID] = client.Send
}

func (r *Room) handleLeave(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.users, userID)
	if send, exists := r.clients[userID]; exists {
		delete(r.clients, userID)
		close(send)
	}
}

func (r *Room) handleBroadcast(msg interface{}) {
	r.mu.RLock()
	clients := make([]chan<- interface{}, 0, len(r.clients))
	for _, send := range r.clients {
		clients = append(clients, send)
	}
	r.mu.RUnlock()

	for _, send := range clients {
		select {
		case send <- msg:
		case <-time.After(100 * time.Millisecond):
			// If client is slow, skip this message to avoid blocking
		}
	}
}

func (r *Room) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.clients = make(map[string]chan<- interface{})
	r.users = make(map[string]*User)
}

// GetUserCount returns the number of users in the room
func (r *Room) GetUserCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.users)
}

// GetUsers returns a copy of users in the room
func (r *Room) GetUsers() map[string]*User {
	r.mu.RLock()
	defer r.mu.RUnlock()

	usersCopy := make(map[string]*User, len(r.users))
	for k, v := range r.users {
		usersCopy[k] = v
	}
	return usersCopy
}
