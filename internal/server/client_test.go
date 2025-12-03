package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/arturskrzydlo/chat-room/internal/messages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCoordinator implements CoordinatorPort
type mockCoordinator struct {
	createCalls []struct {
		roomID, authorID, roomName string
		send                       chan<- interface{}
	}
	joinCalls []struct {
		roomID, userID, userName string
		send                     chan<- interface{}
	}
	leaveCalls []struct {
		roomID, userID string
	}
	sendMsgCalls []struct {
		roomID, userID, content string
	}

	createErr error
	joinErr   error
	leaveErr  error
	sendErr   error
}

func (m *mockCoordinator) CreateRoom(roomID, authorID, roomName string, send chan<- interface{}) error {
	m.createCalls = append(m.createCalls, struct {
		roomID, authorID, roomName string
		send                       chan<- interface{}
	}{roomID, authorID, roomName, send})
	return m.createErr
}

func (m *mockCoordinator) JoinRoom(roomID, userID, userName string, send chan<- interface{}) error {
	m.joinCalls = append(m.joinCalls, struct {
		roomID, userID, userName string
		send                     chan<- interface{}
	}{roomID, userID, userName, send})
	return m.joinErr
}

func (m *mockCoordinator) LeaveRoom(roomID, userID string) error {
	m.leaveCalls = append(m.leaveCalls, struct {
		roomID, userID string
	}{roomID, userID})
	return m.leaveErr
}

func (m *mockCoordinator) SendMessage(roomID, userID, content string) error {
	m.sendMsgCalls = append(m.sendMsgCalls, struct {
		roomID, userID, content string
	}{roomID, userID, content})
	return m.sendErr
}

func newTestClientWithMock(t *testing.T, mc *mockCoordinator) *Client {
	t.Helper()
	// nil *websocket.Conn is fine because we only test handlers writing to c.send
	c := &Client{
		rooms:       make(map[string]struct{}),
		send:        make(chan interface{}, 32),
		coordinator: mc,
		ctx:         context.Background(),
		cancel:      func() {},
	}
	return c
}

func mustRaw(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestClientEnsureIdentity(t *testing.T) {
	c := newTestClientWithMock(t, &mockCoordinator{})

	// first bind ok
	require.NoError(t, c.ensureIdentity("user1", "User One"))
	assert.Equal(t, "user1", c.userID)
	assert.Equal(t, "User One", c.userName)

	// same id again ok
	require.NoError(t, c.ensureIdentity("user1", "User One Again"))

	// different id should error
	err := c.ensureIdentity("user2", "User Two")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection already bound")
}

func TestClientHandleCreateRoomSuccess(t *testing.T) {
	mc := &mockCoordinator{}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))

	payload := messages.CreateRoomPayload{
		RoomID:   "room_1",
		RoomName: "Room One",
		UserID:   "user1",
		UserName: "User One",
	}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeCreateRoom,
		Payload: mustRaw(payload),
	}

	c.handleCreateRoom(&wsMsg)

	require.Len(t, mc.createCalls, 1)
	assert.Equal(t, "room_1", mc.createCalls[0].roomID)
	assert.Equal(t, "user1", mc.createCalls[0].authorID)
	assert.Equal(t, "Room One", mc.createCalls[0].roomName)
}

func TestClientHandleCreateRoomError(t *testing.T) {
	mc := &mockCoordinator{createErr: errors.New("boom")}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))

	payload := messages.CreateRoomPayload{
		RoomID:   "room_1",
		RoomName: "Room One",
		UserID:   "user1",
		UserName: "User One",
	}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeCreateRoom,
		Payload: mustRaw(payload),
	}

	c.handleCreateRoom(&wsMsg)

	ev := <-c.send
	errEv, ok := ev.(messages.ErrorPayload)
	require.True(t, ok)
	assert.Equal(t, "create_room_error", errEv.Code)
}

func TestClientHandleJoinRoomSuccess(t *testing.T) {
	mc := &mockCoordinator{}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))

	payload := messages.JoinRoomPayload{
		RoomID:   "room_1",
		UserID:   "user1",
		UserName: "User One",
	}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeJoin,
		Payload: mustRaw(payload),
	}

	c.handleJoinRoom(&wsMsg)

	require.Len(t, mc.joinCalls, 1)
	assert.Equal(t, "room_1", mc.joinCalls[0].roomID)
	assert.Equal(t, "user1", mc.joinCalls[0].userID)
	assert.Equal(t, "User One", mc.joinCalls[0].userName)

	_, ok := c.rooms["room_1"]
	assert.True(t, ok, "expected room_1 in client.rooms")

	ev := <-c.send
	js, ok := ev.(messages.JoinSuccess)
	require.True(t, ok)
	assert.Equal(t, "join_success", js.Type)
	assert.Equal(t, "room_1", js.RoomID)
	assert.Equal(t, "user1", js.UserID)
}

func TestClientHandleJoinRoomError(t *testing.T) {
	mc := &mockCoordinator{joinErr: errors.New("join-fail")}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))

	payload := messages.JoinRoomPayload{
		RoomID:   "room_1",
		UserID:   "user1",
		UserName: "User One",
	}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeJoin,
		Payload: mustRaw(payload),
	}

	c.handleJoinRoom(&wsMsg)

	ev := <-c.send
	errEv, ok := ev.(messages.ErrorPayload)
	require.True(t, ok)
	assert.Equal(t, "join_room_error", errEv.Code)
}

func TestClientHandleLeaveRoomSuccess(t *testing.T) {
	mc := &mockCoordinator{}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))
	c.rooms["room_1"] = struct{}{}

	payload := messages.LeaveRoomPayload{RoomID: "room_1"}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeLeave,
		Payload: mustRaw(payload),
	}

	c.handleLeaveRoom(&wsMsg)

	require.Len(t, mc.leaveCalls, 1)
	assert.Equal(t, "room_1", mc.leaveCalls[0].roomID)
	assert.Equal(t, "user1", mc.leaveCalls[0].userID)

	_, ok := c.rooms["room_1"]
	assert.False(t, ok, "expected room_1 removed from client.rooms")
}

func TestClientHandleLeaveRoomError(t *testing.T) {
	mc := &mockCoordinator{leaveErr: errors.New("leave-fail")}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))
	c.rooms["room_1"] = struct{}{}

	payload := messages.LeaveRoomPayload{RoomID: "room_1"}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeLeave,
		Payload: mustRaw(payload),
	}

	c.handleLeaveRoom(&wsMsg)

	ev := <-c.send
	errEv, ok := ev.(messages.ErrorPayload)
	require.True(t, ok)
	assert.Equal(t, "leave_room_error", errEv.Code)
}

func TestClientHandleLeaveRoomNotInRoom(t *testing.T) {
	mc := &mockCoordinator{}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))

	payload := messages.LeaveRoomPayload{RoomID: "room_1"}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeLeave,
		Payload: mustRaw(payload),
	}

	c.handleLeaveRoom(&wsMsg)

	ev := <-c.send
	errEv, ok := ev.(messages.ErrorPayload)
	require.True(t, ok)
	assert.Equal(t, "leave_room_error", errEv.Code)
	assert.Equal(t, "user not in this room", errEv.Message)
}

func TestClientHandleChatMessageSuccess(t *testing.T) {
	mc := &mockCoordinator{}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))
	c.rooms["room_1"] = struct{}{}

	payload := messages.MessagePayload{
		RoomID:  "room_1",
		Message: "hello",
	}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeMessage,
		Payload: mustRaw(payload),
	}

	c.handleChatMessage(&wsMsg)

	require.Len(t, mc.sendMsgCalls, 1)
	assert.Equal(t, "room_1", mc.sendMsgCalls[0].roomID)
	assert.Equal(t, "user1", mc.sendMsgCalls[0].userID)
	assert.Equal(t, "hello", mc.sendMsgCalls[0].content)
}

func TestClientHandleChatMessageNoRoomID(t *testing.T) {
	mc := &mockCoordinator{}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))

	payload := messages.MessagePayload{
		RoomID:  "",
		Message: "hello",
	}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeMessage,
		Payload: mustRaw(payload),
	}

	c.handleChatMessage(&wsMsg)

	ev := <-c.send
	errEv, ok := ev.(messages.ErrorPayload)
	require.True(t, ok)
	assert.Equal(t, "message_error", errEv.Code)
	assert.Equal(t, "room_id is required", errEv.Message)
}

func TestClientHandleChatMessageNotInRoom(t *testing.T) {
	mc := &mockCoordinator{}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))

	payload := messages.MessagePayload{
		RoomID:  "room_1",
		Message: "hello",
	}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeMessage,
		Payload: mustRaw(payload),
	}

	c.handleChatMessage(&wsMsg)

	ev := <-c.send
	errEv, ok := ev.(messages.ErrorPayload)
	require.True(t, ok)
	assert.Equal(t, "message_error", errEv.Code)
	assert.Equal(t, "not in this room", errEv.Message)
}

func TestClientHandleChatMessageCoordinatorError(t *testing.T) {
	mc := &mockCoordinator{sendErr: errors.New("send-fail")}
	c := newTestClientWithMock(t, mc)
	require.NoError(t, c.ensureIdentity("user1", "User One"))
	c.rooms["room_1"] = struct{}{}

	payload := messages.MessagePayload{
		RoomID:  "room_1",
		Message: "hello",
	}
	wsMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeMessage,
		Payload: mustRaw(payload),
	}

	c.handleChatMessage(&wsMsg)

	ev := <-c.send
	errEv, ok := ev.(messages.ErrorPayload)
	require.True(t, ok)
	assert.Equal(t, "message_error", errEv.Code)
}

func TestClientCleanupLeavesAllRooms(t *testing.T) {
	mc := &mockCoordinator{}
	c := newTestClientWithMock(t, mc)
	c.userID = "user1"
	c.rooms["room_1"] = struct{}{}
	c.rooms["room_2"] = struct{}{}

	c.cleanup()

	require.Len(t, mc.leaveCalls, 2)
	roomIDs := []string{mc.leaveCalls[0].roomID, mc.leaveCalls[1].roomID}
	assert.Contains(t, roomIDs, "room_1")
	assert.Contains(t, roomIDs, "room_2")
}
