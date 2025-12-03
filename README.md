# Chat Room - WebSocket Server

A chat room server built in Go using WebSockets. Multiple users can create and join rooms, exchange real-time messages, and receive system notifications.

---

## Running the Server

```bash
go run ./cmd/main/main.go
```

Server listens on `http://localhost:8080`
- WebSocket endpoint: `ws://localhost:8080/ws`
- Health check: `http://localhost:8080/health`

---

## Architecture

**WsServer** - HTTP handler for WebSocket upgrades; manages client registry.

**Client** - Per-connection handler with two goroutines: `readPump` (blocks on read) and `writePump` (sends messages). Each client binds to a user identity once.

**Coordinator** - Central registry of rooms; orchestrates room lifecycle (create, join, leave).

**Room** - Single goroutine per room running an event loop. Processes join/leave/broadcast sequentially; maintains user list and client send channels.

Why event loops? Sequential processing eliminates race conditions, simplifies reasoning about state, and provides natural backpressure handling without mutex contention.

---

## API

### WebSocket Endpoint

```
ws://localhost:8080/ws
```

All messages are JSON: `{ "type": "action_type", "payload": {...} }`

### Message Examples

**Create Room**
```json
{
  "type": "create_room",
  "payload": {
    "room_id": "room_1",
    "room_name": "hello room",
    "user_id": "Artur",
    "user_name": "Artur"
  }
}
```

**Join Room**
```json
{
  "type": "join",
  "payload": {
    "room_id": "room_1",
    "user_id": "Michal",
    "user_name": "Michal"
  }
}
```

**Send Message**
```json
{
  "type": "message",
  "payload": {
    "room_id": "room_1",
    "message": "hello everybody"
  }
}
```

**Leave Room**
```json
{
  "type": "leave",
  "payload": {
    "room_id": "room_1"
  }
}
```

**Ping**
```json
{
  "type": "ping",
  "payload": null
}
```

---

## Potential Improvements

**Rate Limiting** - Prevent message flooding: token bucket per client, connection limits per IP.

**Schema Validation** - Enforce min/max lengths, character sets on user IDs and room names using validator library.

**Idle Connection Removal** - Close connections idle >5 minutes.

**Authentication** - Current design trusts client-provided user IDs (no database). Add some authentication

**Busy Rooms** - Broadcast to 1000+ users is sequential. Can spawn per-client goroutines or use async distribution to avoid blocking on slow clients.

---
