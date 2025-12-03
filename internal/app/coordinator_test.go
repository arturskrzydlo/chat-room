package app

import (
	"context"
	"testing"
	"time"

	"github.com/arturskrzydlo/chat-room/internal/messages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoordinatorCreateRoom(t *testing.T) {
	c := NewCoordinator()
	send := make(chan interface{}, 10)

	err := c.CreateRoom("room_1", "author1", "Room One", send)
	require.NoError(t, err)

	// Room exists with correct fields.
	room := c.GetRoom("room_1")
	require.NotNil(t, room)
	assert.Equal(t, "room_1", room.ID)
	assert.Equal(t, "Room One", room.Name)
	assert.Equal(t, "author1", room.AuthorID)

	// Author is auto-joined.
	time.Sleep(10 * time.Millisecond)
	users := room.GetUsers()
	_, ok := users["author1"]
	assert.True(t, ok, "expected author1 to be in room users after CreateRoom")

	// RoomCreateEvent event sent on send channel.
	select {
	case ev := <-send:
		rc, ok := ev.(messages.RoomCreateEvent)
		require.True(t, ok, "expected RoomCreateEvent event")
		assert.Equal(t, "room_1", rc.RoomID)
		assert.Equal(t, "author1", rc.AuthorID)
		assert.Equal(t, "Room One", rc.RoomName)
	default:
		require.Fail(t, "expected RoomCreateEvent event on send channel")
	}

	// Creating the same room again should fail.
	err = c.CreateRoom("room_1", "author1", "Room One", send)
	require.Error(t, err)
}

func TestCoordinatorCreateRoomValidation(t *testing.T) {
	c := NewCoordinator()
	send := make(chan interface{}, 1)

	tests := []struct {
		name     string
		roomID   string
		author   string
		roomName string
		wantErr  bool
	}{
		{"ok", "room_ok", "author1", "Room", false},
		{"empty room id", "", "author1", "Room", true},
		{"empty room name", "room_no_name", "author1", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.CreateRoom(tt.roomID, tt.author, tt.roomName, send)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}

	// duplicate id case (fresh coordinator)
	c = NewCoordinator()
	send = make(chan interface{}, 1)
	err := c.CreateRoom("dup", "author", "Room", send)
	require.NoError(t, err)
	err = c.CreateRoom("dup", "author", "Room", send)
	require.Error(t, err)
}

func TestCoordinatorJoinRoom(t *testing.T) {
	c := NewCoordinator()
	sendAuthor := make(chan interface{}, 10)
	sendUser2 := make(chan interface{}, 10)

	require.NoError(t, c.CreateRoom("room_1", "author1", "Room One", sendAuthor))

	// Join second user.
	err := c.JoinRoom("room_1", "user2", "User Two", sendUser2)
	require.NoError(t, err)

	room := c.GetRoom("room_1")
	require.NotNil(t, room)

	// Wait for join to be processed.
	time.Sleep(10 * time.Millisecond)
	users := room.GetUsers()
	_, ok := users["user2"]
	assert.True(t, ok, "expected user2 to be in room after JoinRoom")

	// UserJoinedEvent broadcast to room participants.
	gotJoinEvent := false
	checkForJoin := func(ch <-chan interface{}) {
		select {
		case ev := <-ch:
			uj, ok := ev.(messages.UserJoinedEvent)
			if ok &&
				uj.Type == messages.EventUserJoinedRoom &&
				uj.RoomID == "room_1" &&
				uj.UserID == "user2" &&
				uj.UserName == "User Two" {
				gotJoinEvent = true
			}
		default:
		}
	}

	checkForJoin(sendAuthor)
	checkForJoin(sendUser2)
	assert.True(t, gotJoinEvent, "expected UserJoinedEvent broadcast to at least one client")

	// Joining same user again should error.
	err = c.JoinRoom("room_1", "user2", "User Two", sendUser2)
	require.Error(t, err)
}

func TestCoordinatorSendMessage(t *testing.T) {
	c := NewCoordinator()
	sendAuthor := make(chan interface{}, 10)
	sendUser2 := make(chan interface{}, 10)

	require.NoError(t, c.CreateRoom("room_1", "author1", "Room One", sendAuthor))
	require.NoError(t, c.JoinRoom("room_1", "user2", "User Two", sendUser2))

	// wait until room.Run has processed the join
	waitForUserInRoom(t, c, "room_1", "user2")

	// Happy path: user2 sends message.
	require.NoError(t, c.SendMessage("room_1", "user2", "hello"))

	expectChatFrom(t, sendAuthor, "user2", "User Two", "hello")
	expectChatFrom(t, sendUser2, "user2", "User Two", "hello")

	// Validation cases.
	require.Error(t, c.SendMessage("room_1", "user2", ""))                              // empty
	require.Error(t, c.SendMessage("room_1", "ghost", "hi"))                            // not in room
	require.Error(t, c.SendMessage("no_room", "user2", "hi"))                           // no such room
	require.Error(t, c.SendMessage("room_1", "user2", string(make([]byte, 10*1024+1)))) // too long
}

func TestCoordinatorSendMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		roomID  string
		userID  string
		content string
		wantErr bool
	}{
		{"ok", "room_1", "user1", "hello", false},
		{"empty content", "room_1", "user1", "", true},
		{"too long", "room_1", "user1", string(make([]byte, 10*1024+1)), true},
		{"no such room", "no_room", "user1", "hi", true},
		{"user not in room", "room_1", "ghost", "hi", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			c := NewCoordinator()
			send := make(chan interface{}, 1)

			// base room + user
			require.NoError(t, c.CreateRoom("room_1", "author1", "Room One", send))
			require.NoError(t, c.JoinRoom("room_1", "user1", "User One", send))

			// wait until join is processed by room.Run
			waitForUserInRoom(t, c, "room_1", "user1")

			err := c.SendMessage(tt.roomID, tt.userID, tt.content)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCoordinatorLeaveRoom(t *testing.T) {
	c := NewCoordinator()
	sendAuthor := make(chan interface{}, 10)
	sendUser2 := make(chan interface{}, 10)

	require.NoError(t, c.CreateRoom("room_1", "author1", "Room One", sendAuthor))
	require.NoError(t, c.JoinRoom("room_1", "user2", "User Two", sendUser2))

	waitForUserInRoom(t, c, "room_1", "user2")

	require.NoError(t, c.LeaveRoom("room_1", "user2"))

	// UserLeftEvent to author
	expectUserLeftEvent(t, sendAuthor, "room_1", "user2", "User Two")

	// author can still leave without error
	require.NoError(t, c.LeaveRoom("room_1", "author1"))
}

func TestCoordinatorShutdown(t *testing.T) {
	c := NewCoordinator()
	send := make(chan interface{}, 10)

	require.NoError(t, c.CreateRoom("room_1", "author1", "Room One", send))
	require.NoError(t, c.CreateRoom("room_2", "author2", "Room Two", send))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.Shutdown(ctx)
	require.NoError(t, err)
}

func waitForUserInRoom(t *testing.T, c *Coordinator, roomID, userID string) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		room := c.GetRoom(roomID)
		require.NotNil(t, room)
		if _, ok := room.GetUsers()[userID]; ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Failf(t, "user not in room", "user %s was not in room %s in time", userID, roomID)
}

func expectChatFrom(t *testing.T, ch <-chan interface{}, wantUserID, wantUserName, wantMsg string) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case ev := <-ch:
			// Only care about RoomMessageEvent; skip others (e.g. RoomCreateEvent, UserJoinedEvent, UserLeftEvent).
			msg, ok := ev.(messages.RoomMessageEvent)
			if !ok {
				continue
			}
			if msg.Type != messages.EventNewMessage {
				continue
			}
			if msg.UserID == wantUserID && msg.UserName == wantUserName && msg.Message.Message == wantMsg {
				return
			}
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	require.Failf(t, "did not see expected chat message",
		"expected message from %s (%s) with content %q", wantUserID, wantUserName, wantMsg)
}

func expectUserLeftEvent(t *testing.T, ch <-chan interface{}, roomID, userID, userName string) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)

	for time.Now().Before(deadline) {
		select {
		case ev := <-ch:
			ul, ok := ev.(messages.UserLeftEvent)
			if !ok {
				continue
			}
			if ul.Type == messages.EventUserLeftRoom &&
				ul.RoomID == roomID &&
				ul.UserID == userID &&
				ul.UserName == userName {
				return
			}
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	assert.Failf(t, "expected UserLeftEvent to be broadcast",
		"did not see UserLeftEvent for room=%q userID=%q userName=%q", roomID, userID, userName)
}
