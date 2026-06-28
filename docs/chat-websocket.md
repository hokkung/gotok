# Chat & WebSocket

Real-time chat built on three pillars: **WebSocket** for client connections, **Redis pub/sub** for cross-instance fan-out, and **PostgreSQL** for persistence. Supports DMs and group conversations with horizontal scaling.

```
                            ┌─────────────────────────────────────┐
  Browser ◄──── WS ────►│           GoTok Instance A           │
  (chat.js)         /ws  │  Hub ──► clients map (user→conns)    │
                          │         │                            │
                          │   ┌─────┴─────┐                     │
                          │   │ ChatService│──► Store ──► Postgres│
                          │   └─────┬─────┘                     │
                          └─────────┼───────────────────────────┘
                                    │ Publish / Subscribe
                              ┌─────▼─────┐
                              │   Redis   │  chat:user:<id>  (pub/sub)
                              │           │  presence:<id>   (key + TTL)
                              └─────▲─────┘
                                    │
                          ┌─────────┼───────────────────────────┐
                          │  Hub ◄──┘   GoTok Instance B        │
                          │   delivers to local WS clients       │
                          └──────────────────────────────────────┘
```

---

## Key Components

| Component | File | Role |
|-----------|------|------|
| **Hub** | `internal/chat/hub.go` | Per-instance connection registry + message routing |
| **ServeWS** | `internal/chat/ws.go` | WebSocket upgrade + read/write pumps |
| **Broker** | `internal/chat/broker.go` | Redis pub/sub + presence (implements `Broker` interface) |
| **ChatService** | `internal/service/chat.go` | Business logic: authz, create/read messages, presence queries |
| **ChatStore** | `internal/store/chat.go` | SQL queries against `conversations`, `messages`, `conversation_participants` |
| **Client (JS)** | `web/static/js/chat.js` | WebSocket lifecycle, send/receive, UI updates |

---

## Connection Lifecycle

### 1. Authentication
The `/ws` endpoint sits behind `RequireAuth()` (`app/app.go:113`). The session cookie is validated **before** the WebSocket upgrade — origin checking is skipped (`InsecureSkipVerify: true`) because the cookie is already verified.

### 2. Upgrade & Register (`ws.go:23-40`)
`ServeWS` upgrades HTTP → WebSocket, creates a `Client` (buffered `send` channel, cap 64), and registers it with the Hub.

### 3. On First Connection (`hub.go:125-148`)
- Subscribes to `chat:user:<userID>` on Redis.
- Sets presence key `presence:<userID>` with 120s TTL.

### 4. On Last Disconnect (`hub.go:152-175`)
- Closes the client's send channel.
- Unsubscribes from Redis.
- Clears the presence key.

### 5. Heartbeat (`hub.go:301-325`)
Every 30s, refreshes the presence TTL for all locally connected users. The 120s TTL ensures crashed instances' users expire automatically.

### 6. Multi-tab Support
The `clients` map is `map[userID]map[*Client]struct{}` — a user can have multiple connections (tabs/devices). The sender is included in message fan-out so all their tabs receive the echo.

---

## Message Flow (Sending a Chat Message)

```
Browser                Instance (sender)              Redis              Instance (recipient)
   │                         │                          │                         │
   │ WS: {message, text}     │                          │                         │
   ├────────────────────────►│                          │                         │
   │                   Read pump decodes                │                         │
   │                   Hub.HandleMessage                │                         │
   │                         │                          │                         │
   │                   ChatService.SendMessage          │                         │
   │                   ├─ requireParticipant (authz)    │                         │
   │                   └─ Store.CreateMessage ──► Postgres                        │
   │                         │                          │                         │
   │                   Marshal {type:"message",...}     │                         │
   │                   Publish to each participant       │                         │
   │                         ├──── chat:user:<A> ──────►│                         │
   │                         │                          ├── deliver ─────────────►│ deliverLocal
   │                         │                          │                         ├─ push to send chan
   │◄── WS echo (sender) ────┤◄── deliver ──────────────┤                         │
   │                         │                          │                 WS write pump
   │                         │                          │◄──────── WS ────────────►│ Browser
```

1. **Client** sends `{type:"message", conversation_id, text}` over WS (`chat.js:265`).
2. **Read pump** decodes the `Envelope` → `hub.HandleMessage` (`ws.go:62`).
3. **`ChatService.SendMessage`** (`service/chat.go:120`) verifies participation, then `Store.CreateMessage` inserts into Postgres and returns the message + participant IDs.
4. **Hub** marshals an outbound `Envelope` and **publishes to every participant's** Redis channel (`hub.go:237-244`), including the sender.
5. **Redis** routes to whichever instance holds the subscriber.
6. **`deliverLocal`** pushes the payload onto each local client's `send` channel (`hub.go:178-195`). If the buffer is full, the message is dropped (already persisted — not lost data).
7. **Write pump** writes it to the WebSocket (`ws.go:77-94`).

**REST fallback**: If the WebSocket isn't open, the client POSTs to `/api/conversations/:id/messages`, which calls the same service and publishes via `hub.PublishToUser` — reusing the identical Redis path.

---

## Read Receipts

1. Client sends `{type:"read", conversation_id, message_id}` (`chat.js:287`).
2. `hub.HandleRead` advances `last_read_msg_id` (via SQL `GREATEST`), then publishes `{type:"read_receipt", ...}` to **other** participants only — sender is skipped (`hub.go:273-276`).
3. Recipients mark the thread as "Seen" (`chat.js:304`).

---

## WebSocket Protocol

JSON frames over `/ws`. The `type` field discriminates.

### Client → Server

| `type` | Fields | Handler |
|--------|--------|---------|
| `message` | `conversation_id`, `text` | `hub.HandleMessage` |
| `read` | `conversation_id`, `message_id` | `hub.HandleRead` |

### Server → Client

| `type` | Fields | Source |
|--------|--------|--------|
| `message` | `id`, `conversation_id`, `sender_id`, `sender_name`, `sender_avatar`, `text`, `created_at` | `hub.HandleMessage` / REST handler |
| `read_receipt` | `conversation_id`, `user_id`, `message_id` | `hub.HandleRead` |

> Limits: read capped at 4 KiB (`readLimit`), write deadline 10s (`writeTimeout`), operation timeout 5s per frame.

---

## Redis Keys & Channels

| Key pattern | Type | Purpose |
|-------------|------|---------|
| `chat:user:<id>` | Pub/Sub channel | Per-user message delivery (messages + read receipts) |
| `presence:<id>` | String key, TTL 120s | Online status — set on connect, refreshed every 30s, deleted on disconnect |

---

## Presence

Presence is **poll-based**, not pushed:
- `SetPresence` / `IsOnline` manage the Redis key (`broker.go:114-129`).
- `ListConversations` enriches each conversation preview with the other user's online status (`service/chat.go:62-66`).
- `GET /api/users/:id/presence` returns `{online: bool}` (`handlers/chat.go:232`).
- The `Envelope` has an `Online` field for `"presence"` events and the client handles them, but the server **never emits** this event type. Presence is only refreshed when the client reloads the conversation list.

---

## Data Model (PostgreSQL)

Defined in `migrations/000002_chat.up.sql`:

```
conversations (id, type ["dm"|"group"], title, created_at)
    │
    ├── conversation_participants (conversation_id, user_id, joined_at, last_read_msg_id)
    │       PK(conversation_id, user_id)
    │
    └── messages (id, conversation_id, sender_id, text, created_at)
            index: (conversation_id, id DESC)
```

All timestamps are Unix epoch `BIGINT`. See `models/models.go:65-111` for Go structs.

---

## Wiring (`app/app.go:27-45`)

```
rdb     = redis.NewClient(...)
broker  = chat.NewRedisBroker(rdb, logger)
chatSvc = service.NewChatService(store, broker)   // broker satisfies PresenceChecker
hub     = chat.NewHub(chatSvc, broker)
go hub.Run(ctx)
go hub.StartPresenceHeartbeat(ctx)
```

The `RedisBroker` implements both `Broker` (for the hub) and `PresenceChecker` (for the service), so the service can query online status without depending on the hub.
