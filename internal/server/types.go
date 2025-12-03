package server

type CoordinatorPort interface {
	CreateRoom(roomID, authorID, roomName string, send chan<- interface{}) error
	JoinRoom(roomID, userID, userName string, send chan<- interface{}) error
	LeaveRoom(roomID, userID string) error
	SendMessage(roomID, userID, content string) error
}
