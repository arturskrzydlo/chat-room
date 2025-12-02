package server_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/arturskrzydlo/chat-room/internal/app"
	"github.com/arturskrzydlo/chat-room/internal/messages"
	"github.com/arturskrzydlo/chat-room/internal/server"
	"github.com/gorilla/websocket"
)

func readJSON(t *testing.T, c *websocket.Conn, v interface{}) {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
}

func mustRaw(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

/*
End‑to‑end happy‑path chat flow between two users over WebSocket.
User 1 connects, establishes identity, and creates a new room.
User 2 connects, establishes identity, and joins the existing room.
Both users exchange chat messages and each message is broadcast to all room participants.
User 2 leaves the room and User 1 receives a system notification that User 2 left.
User 1 then leaves the room, and the room is shut down cleanly by the coordinator.
*/
func TestChatEndToEnd(t *testing.T) {
	coord := app.NewCoordinator()
	wsSrv := server.NewWsServer(coord)
	ts := httptest.NewServer(wsSrv)
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	u.Scheme = "ws"

	dialer := websocket.Dialer{}

	// Two WS clients: user1 (creator) and user2 (joiner).
	conn1, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial user1: %v", err)
	}
	defer conn1.Close()

	conn2, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial user2: %v", err)
	}
	defer conn2.Close()

	// 1) user1 creates room_1 (and establishes identity).
	createPayload := messages.CreateRoomPayload{
		RoomID:   "room_1",
		RoomName: "hello room",
		UserID:   "user1",
		UserName: "User One",
	}
	createMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeCreateRoom,
		Payload: mustRaw(createPayload),
	}
	if err := conn1.WriteJSON(createMsg); err != nil {
		t.Fatalf("user1 create room write: %v", err)
	}

	// user1 should receive new_room event.
	var ev map[string]interface{}
	readJSON(t, conn1, &ev)
	if ev["type"] != string(messages.EventNewRoom) {
		t.Fatalf("expected new_room event for user1, got: %#v", ev)
	}

	// 2) user2 joins room_1.
	joinPayload2 := messages.JoinRoomPayload{
		RoomID:   "room_1",
		UserID:   "user2",
		UserName: "User Two",
	}
	joinMsg2 := messages.WsMessage{
		Type:    messages.MessageActionTypeJoin,
		Payload: mustRaw(joinPayload2),
	}
	if err := conn2.WriteJSON(joinMsg2); err != nil {
		t.Fatalf("user2 join write: %v", err)
	}

	// user2 expects join_success.
	var joinResp map[string]interface{}
	readJSON(t, conn2, &joinResp)
	if joinResp["type"] != "join_success" {
		t.Fatalf("expected join_success for user2, got: %#v", joinResp)
	}

	// user1 receives system "User Two joined the room".
	var sysJoin messages.Message
	readJSON(t, conn1, &sysJoin)
	if sysJoin.Type != messages.EventNewMessage || sysJoin.UserID != "system" {
		t.Fatalf("expected system join message for user1, got: %#v", sysJoin)
	}
	if sysJoin.Message.Message != "User Two joined the room" {
		t.Fatalf("unexpected join message content: %#v", sysJoin)
	}

	// 3) user1 sends a chat message.
	msgPayload1 := messages.MessagePayload{Message: "hello from user1"}
	sendMsg1 := messages.WsMessage{
		Type:    messages.MessageActionTypeMessage,
		Payload: mustRaw(msgPayload1),
	}
	if err := conn1.WriteJSON(sendMsg1); err != nil {
		t.Fatalf("user1 send message write: %v", err)
	}

	// On conn1, next message: user1's own chat.
	var msgEv1 messages.Message
	readJSON(t, conn1, &msgEv1)
	if msgEv1.Type != messages.EventNewMessage || msgEv1.UserID != "user1" ||
		msgEv1.Message.Message != "hello from user1" {
		t.Fatalf("user1 got wrong first chat message: %#v", msgEv1)
	}

	// On conn2, next message is still the system join broadcast ("User Two joined the room").
	var sysJoinOn2 messages.Message
	readJSON(t, conn2, &sysJoinOn2)
	if sysJoinOn2.Type != messages.EventNewMessage || sysJoinOn2.UserID != "system" ||
		sysJoinOn2.Message.Message != "User Two joined the room" {
		t.Fatalf("expected system join message for user2, got: %#v", sysJoinOn2)
	}

	// The following message on conn2 is the chat from user1.
	var msgEv2 messages.Message
	readJSON(t, conn2, &msgEv2)
	if msgEv2.Type != messages.EventNewMessage || msgEv2.UserID != "user1" ||
		msgEv2.Message.Message != "hello from user1" {
		t.Fatalf("user2 got wrong first chat message: %#v", msgEv2)
	}

	// 4) user2 replies.
	msgPayload2 := messages.MessagePayload{Message: "hi from user2"}
	sendMsg2 := messages.WsMessage{
		Type:    messages.MessageActionTypeMessage,
		Payload: mustRaw(msgPayload2),
	}
	if err := conn2.WriteJSON(sendMsg2); err != nil {
		t.Fatalf("user2 send message write: %v", err)
	}

	// Next message on conn2: its own chat.
	readJSON(t, conn2, &msgEv2)
	if msgEv2.Type != messages.EventNewMessage || msgEv2.UserID != "user2" ||
		msgEv2.Message.Message != "hi from user2" {
		t.Fatalf("user2 got wrong reply chat message: %#v", msgEv2)
	}

	// Next message on conn1: reply from user2.
	readJSON(t, conn1, &msgEv1)
	if msgEv1.Type != messages.EventNewMessage || msgEv1.UserID != "user2" ||
		msgEv1.Message.Message != "hi from user2" {
		t.Fatalf("user1 got wrong reply chat message: %#v", msgEv1)
	}

	// 5) both users leave room_1.
	leavePayload := messages.LeaveRoomPayload{RoomID: "room_1"}
	leaveMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeLeave,
		Payload: mustRaw(leavePayload),
	}

	// user2 leaves first.
	if err := conn2.WriteJSON(leaveMsg); err != nil {
		t.Fatalf("user2 leave write: %v", err)
	}

	// user1 should receive system "User Two left the room".
	var sysLeave messages.Message
	readJSON(t, conn1, &sysLeave)
	if sysLeave.Type != messages.EventNewMessage || sysLeave.UserID != "system" {
		t.Fatalf("expected system leave message for user1, got: %#v", sysLeave)
	}
	if sysLeave.Message.Message != "User Two left the room" {
		t.Fatalf("unexpected leave message content: %#v", sysLeave)
	}

	// now user1 leaves
	if err := conn1.WriteJSON(leaveMsg); err != nil {
		t.Fatalf("user1 leave write: %v", err)
	}

	// Graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := coord.Shutdown(ctx); err != nil {
		t.Fatalf("coordinator shutdown: %v", err)
	}
}
