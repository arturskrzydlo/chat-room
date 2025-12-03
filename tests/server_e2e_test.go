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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readJSON(t *testing.T, c *websocket.Conn, v interface{}) {
	t.Helper()
	err := c.SetReadDeadline(time.Now().Add(2 * time.Second))
	require.NoError(t, err, "SetReadDeadline")

	_, data, err := c.ReadMessage()
	require.NoError(t, err, "ReadMessage")

	err = json.Unmarshal(data, v)
	require.NoError(t, err, "Unmarshal")
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
	require.NoError(t, err, "parse test server url")

	u.Scheme = "ws"
	dialer := websocket.Dialer{}

	// Two WS clients: user1 (creator) and user2 (joiner).
	conn1, _, err := dialer.Dial(u.String(), nil)
	require.NoError(t, err, "dial user1")
	defer conn1.Close()

	conn2, _, err := dialer.Dial(u.String(), nil)
	require.NoError(t, err, "dial user2")
	defer conn2.Close()

	userID1 := "user1"
	userID2 := "user2"

	// 1) user1 creates room_1 (and establishes identity).
	createPayload := messages.CreateRoomPayload{
		RoomID:   "room_1",
		RoomName: "hello room",
		UserID:   userID1,
		UserName: "User One",
	}
	createMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeCreateRoom,
		Payload: mustRaw(createPayload),
	}
	err = conn1.WriteJSON(createMsg)
	require.NoError(t, err, "user1 create room write")

	// user1 should receive new_room event.
	var ev map[string]interface{}
	readJSON(t, conn1, &ev)
	require.Equal(t, string(messages.EventNewRoom), ev["type"], "expected new_room event for user1")

	// 2) user2 joins room_1.
	joinPayload2 := messages.JoinRoomPayload{
		RoomID:   "room_1",
		UserID:   userID2,
		UserName: "User Two",
	}
	joinMsg2 := messages.WsMessage{
		Type:    messages.MessageActionTypeJoin,
		Payload: mustRaw(joinPayload2),
	}
	err = conn2.WriteJSON(joinMsg2)
	require.NoError(t, err, "user2 join write")

	// user2 expects join_success.
	var joinResp map[string]interface{}
	readJSON(t, conn2, &joinResp)
	require.Equal(t, "join_success", joinResp["type"], "expected join_success for user2")

	// user1 receives system "User Two joined the room".
	var sysJoin messages.UserJoinedEvent
	readJSON(t, conn1, &sysJoin)
	require.Equal(t, messages.EventUserJoinedRoom, sysJoin.Type, "expected system join event type")
	require.Equal(t, userID2, sysJoin.UserID, "expected system join user id")

	// 3) user1 sends a chat message.
	msgPayload1 := messages.MessagePayload{Message: "hello from user1"}
	sendMsg1 := messages.WsMessage{
		Type:    messages.MessageActionTypeMessage,
		Payload: mustRaw(msgPayload1),
	}
	err = conn1.WriteJSON(sendMsg1)
	require.NoError(t, err, "user1 send message write")

	// On conn1, next message: user1's own chat.
	var msgEv1 messages.RoomMessageEvent
	readJSON(t, conn1, &msgEv1)
	require.Equal(t, messages.EventNewMessage, msgEv1.Type, "user1 first chat type")
	require.Equal(t, userID1, msgEv1.UserID, "user1 first chat user id")
	require.Equal(t, "hello from user1", msgEv1.Message.Message, "user1 first chat content")

	// On conn2, next message is still the system join broadcast ("User Two joined the room").
	var sysJoinOn2 messages.UserJoinedEvent
	readJSON(t, conn2, &sysJoinOn2)
	require.Equal(t, messages.EventUserJoinedRoom, sysJoinOn2.Type, "user2 system join type")
	require.Equal(t, userID2, sysJoinOn2.UserID, "user2 system join user id")

	// The following message on conn2 is the chat from user1.
	var msgEv2 messages.RoomMessageEvent
	readJSON(t, conn2, &msgEv2)
	require.Equal(t, messages.EventNewMessage, msgEv2.Type, "user2 first chat type")
	require.Equal(t, userID1, msgEv2.UserID, "user2 first chat user id")
	require.Equal(t, "hello from user1", msgEv2.Message.Message, "user2 first chat content")

	// 4) user2 replies.
	msgPayload2 := messages.MessagePayload{Message: "hi from user2"}
	sendMsg2 := messages.WsMessage{
		Type:    messages.MessageActionTypeMessage,
		Payload: mustRaw(msgPayload2),
	}
	err = conn2.WriteJSON(sendMsg2)
	require.NoError(t, err, "user2 send message write")

	// Next message on conn2: its own chat.
	readJSON(t, conn2, &msgEv2)
	require.Equal(t, messages.EventNewMessage, msgEv2.Type, "user2 reply type")
	require.Equal(t, userID2, msgEv2.UserID, "user2 reply user id")
	require.Equal(t, "hi from user2", msgEv2.Message.Message, "user2 reply content")

	// Next message on conn1: reply from user2.
	readJSON(t, conn1, &msgEv1)
	require.Equal(t, messages.EventNewMessage, msgEv1.Type, "user1 sees reply type")
	require.Equal(t, userID2, msgEv1.UserID, "user1 sees reply user id")
	require.Equal(t, "hi from user2", msgEv1.Message.Message, "user1 sees reply content")

	// 5) both users leave room_1.
	leavePayload := messages.LeaveRoomPayload{RoomID: "room_1"}
	leaveMsg := messages.WsMessage{
		Type:    messages.MessageActionTypeLeave,
		Payload: mustRaw(leavePayload),
	}

	// user2 leaves first.
	err = conn2.WriteJSON(leaveMsg)
	require.NoError(t, err, "user2 leave write")

	// user1 should receive system "User Two left the room".
	var sysLeave messages.RoomMessageEvent
	readJSON(t, conn1, &sysLeave)
	require.Equal(t, messages.EventUserLeftRoom, sysLeave.Type, "user1 system leave type")
	require.Equal(t, userID2, sysLeave.UserID, "user1 system leave user id")

	// now user1 leaves
	err = conn1.WriteJSON(leaveMsg)
	require.NoError(t, err, "user1 leave write")

	// Graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = coord.Shutdown(ctx)
	assert.NoError(t, err, "coordinator shutdown")
}
