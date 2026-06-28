# GoTok — Knowledge Transfer / Architecture Guide

A complete walkthrough of how the **GoTok** system works. Read this top‑to‑bottom and you should be able to navigate, modify, and extend the codebase confidently.

---

## 1. What is GoTok?

GoTok is a **TikTok‑style vertical‑video web app with real‑time chat**, written in Go.

- **Browsing is anonymous; interactions require login.** Every visitor can browse the feed and read comments freely. **Liking, commenting, uploading, and chatting are gated behind a session login** (SSO via Google/Facebook is stubbed; a demo login is wired up so the flow is testable now).
- **Vertical "for you" feed** — full‑screen videos that auto‑play as you scroll, snap one per screen, mute/unmute on tap, like on double‑tap.
- **Upload your own videos** (mp4 / webm / mov / mkv, up to 200 MB), stored on local disk and **attributed to the uploading user**.
- **User profiles** (`/u/:id`) — every video records its uploader; a profile page lists a creator's videos in a 3‑column grid via `ListVideosByUser`.
- **Likes, comments, view counting**, and infinite scroll pagination — all backed by PostgreSQL.
- **Real‑time chat** — 1‑on‑1 DMs and group conversations over WebSocket, with **horizontal scaling** via Redis pub/sub for cross‑instance message routing and presence tracking.

The system is designed for multi‑instance deployment: PostgreSQL for shared state, Redis for pub/sub + presence, multiple GoTok instances behind a load balancer.

---

## 2. Tech Stack

| Layer | Technology | Notes |
|-------|------------|-------|
| Language | **Go 1.25** | `cmd/gotok` entry point + `internal/*` packages. |
| Web framework | **Gin** (`github.com/gin-gonic/gin v1.12.0`) | HTTP routing, middleware, HTML templates, multipart upload. |
| Database | **PostgreSQL** via `pgx/v5/stdlib` | Replaces SQLite. Supports concurrent writers across instances. Connection pool: 25 open / 10 idle. |
| Migrations | **golang‑migrate** (`v4`) | Embedded SQL files in `internal/store/migrations/`. Runs automatically on startup. |
| Message broker | **Redis** (`go‑redis/v9`) | Pub/sub for WebSocket cross‑instance delivery + presence keys with TTL. |
| WebSocket | **coder/websocket** (`v1.8`) | Context‑native WS library. Read/write pumps with deadlines + size limits. |
| Templating | Gin's built‑in `html/template` loader | `LoadHTMLGlob("web/templates/*")`. |
| Frontend | **Vanilla HTML/CSS/JS** (no framework) | `fetch` + DOM manipulation, `IntersectionObserver`, CSS scroll‑snap, native `WebSocket`. |
| Storage | Local filesystem | Uploaded videos live under `data/uploads/`. |
| Logging | **Zap** (`go.uber.org/zap`) | Structured logging throughout. |

---

## 3. Project Structure

```
gotok/
├── cmd/
│   └── gotok/main.go          # Entry point: config → store → redis → app.Run(ctx)
├── go.mod / go.sum            # Module `github.com/hokkung/gotok`, Go 1.25
├── Makefile                   # Dev tasks: run, build, up, down, vet, test, test-race
├── Dockerfile                 # Multi-stage build (golang → alpine)
├── docker-compose.yml         # PostgreSQL + Redis + 2 GoTok instances (horizontal scaling)
├── example.env                # Config template (GOTOK_DATABASE_URL, GOTOK_REDIS_ADDR, …)
│
├── internal/                  # All non‑main Go code (Go's "internal" import protection)
│   ├── app/app.go             # Gin engine + Redis client + chat hub + route registration + graceful shutdown
│   ├── config/config.go       # App config (PostgreSQL DSN, Redis addr, upload dir, cookie secret)
│   ├── models/models.go       # Plain structs: Video, User, Comment, Conversation, Message, …
│   ├── store/
│   │   ├── store.go           # PostgreSQL layer: open, migrate (golang-migrate), all SQL queries (with ctx)
│   │   ├── chat.go            # Chat store methods: conversations, messages, participants, read receipts
│   │   ├── embed.go           # //go:embed migrations/*.sql
│   │   └── migrations/        # Versioned SQL migration files
│   │       ├── 000001_init.up.sql / .down.sql       # videos, likes, comments, users, sessions
│   │       └── 000002_chat.up.sql / .down.sql       # conversations, conversation_participants, messages
│   ├── chat/                  # Real‑time chat layer (WebSocket hub + Redis broker)
│   │   ├── hub.go             # Per‑instance connection manager (register/unregister/deliver)
│   │   ├── broker.go          # Redis pub/sub: per‑user channels + presence keys
│   │   └── ws.go              # WebSocket handler: read/write pumps with deadlines
│   ├── middleware/
│   │   ├── auth.go            # Session auth: loads user from cookie on every request
│   │   └── logger.go          # Zap‑backed access logging + panic recovery
│   └── handlers/              # HTTP handlers (one concern per file)
│       ├── handlers.go        #   Handlers struct + constructor (cfg, store, hub, logger)
│       ├── helpers.go         #   randID() helper
│       ├── feed.go            #   FeedPage + ListVideos
│       ├── upload.go          #   UploadPage + Upload
│       ├── video.go           #   ServeFile (Range‑aware)
│       ├── like.go            #   ToggleLike + View
│       ├── comment.go         #   ListComments + CreateComment (now returns user_id + avatar_url)
│       ├── auth.go            #   Login/Logout/Me/Register + session management
│       ├── profile.go         #   ProfilePage + EditProfile + ListLikedVideos
│       └── chat.go            #   ChatPage + chat REST API + WebSocket upgrade
│
├── web/                       # Frontend assets
│   ├── templates/
│   │   ├── layout.html        #   Header/footer partials + sidebar nav
│   │   ├── feed.html          #   The feed shell; loads feed.js
│   │   ├── upload.html        #   Upload form; loads upload.js
│   │   ├── profile.html       #   Profile page (with Message button); loads profile.js
│   │   ├── chat.html          #   Chat UI (conversation list + message thread); loads chat.js
│   │   └── login.html         #   Login page
│   └── static/
│       ├── css/style.css      # All styling (feed, chat, comments, profiles)
│       └── js/
│           ├── feed.js        # Feed rendering, gestures, likes, clickable comments
│           ├── upload.js      # Drag‑and‑drop + form submit
│           ├── profile.js     # Profile tabs + edit modal + Message button
│           ├── chat.js        # WebSocket client, conversation list, message thread
│           ├── nav.js         # Hamburger sidebar toggle
│           └── login.js       # Login form helpers
│
└── data/                      # Runtime data (created automatically; do NOT commit)
    ├── cookie_secret          # 32‑byte hex secret
    └── uploads/               # Uploaded video + avatar files
```

> **Convention:** business logic lives in `internal/`, split by *concern* (app / config / models / store / chat / middleware / handlers). Handlers are further split by *feature* (feed, upload, like, comment, video, auth, profile, chat). Follow this pattern when adding new features.

---

## 4. High‑Level Architecture & Request Flow

### Startup (`cmd/gotok/main.go` → `internal/app/app.go`)

```
config.Load() → logger.New() → signal.NotifyContext(SIGINT, SIGTERM)
  → store.New(ctx, databaseURL, lg)     [PostgreSQL + golang-migrate]
  → app.Run(ctx, cfg, st, lg):
      redis.NewClient(ping)
      chat.NewRedisBroker(rdb)
      chat.NewHub(st, broker)
      go hub.Run(ctx)                   [event loop: register/unregister/deliver]
      go hub.StartPresenceHeartbeat(ctx) [30s TTL refresh]
      gin.New() + middleware + routes
      go srv.ListenAndServe(:8080)
      <-ctx.Done()                      [blocks until SIGINT/SIGTERM]
      srv.Shutdown(10s timeout)         [graceful: drain HTTP, close hub, close Redis]
```

1. **`config.Load()`** — reads `.env` / environment; ensures `data/` and `data/uploads/` exist; generates and persists a 32‑byte `cookie_secret` on first run.
2. **`signal.NotifyContext`** — creates an app‑level context cancelled on `SIGINT`/`SIGTERM`, enabling graceful shutdown.
3. **`store.New(ctx, databaseURL, lg)`** — opens PostgreSQL via `pgx/v5/stdlib`, configures connection pool (`SetMaxOpenConns(25)`, `SetMaxIdleConns(10)`, 5‑min lifetime), pings, runs migrations via golang‑migrate.
4. **Redis + Chat Hub** — creates a Redis client (pings to verify connectivity), wraps it in a `RedisBroker`, creates a `Hub` wired to the store + broker, starts the hub event loop and presence heartbeat goroutines.
5. **Gin setup** — `gin.New()` + zap‑backed middleware, `Auth()` on every request, HTML templates, `/static`, routes.
6. **Graceful shutdown** — on signal: `srv.Shutdown()` drains in‑flight HTTP, hub closes all WS connections and unsubscribes from Redis, Redis client closes.

### A typical feed request

```
Browser (session cookie) ──► GET /api/videos?cursor=N&limit=20
   │
   ├─ middleware.Auth(): read session cookie → store.GetUserBySession(ctx, token) → load user
   ├─ handlers.ListVideos(): read userID + cursor + limit
   │     └─ store.ListVideos(ctx, userID, cursor, limit) → SQL EXISTS() for per‑user like state
   └─ responds JSON { videos: [...], next: <last id or 0> }
```

### Horizontal scaling: chat message flow

```
User A (Instance :8080) sends message via WebSocket
  → hub.HandleMessage(ctx, senderID, env):
      1. store.IsParticipant(ctx, convID, senderID) → verify access
      2. store.CreateMessage(ctx, convID, senderID, text) → persist to PostgreSQL
      3. broker.Publish(recipientID, payload) for each participant → Redis pub/sub
         (including the sender — so multi‑tab/multi‑device works)
  → Instance :8081 Redis subscriber receives the payload
  → hub.deliverLocal(recipientID, payload) → User B's WebSocket on Instance :8081

Presence: SET presence:<userID> EX 120 on connect, refreshed every 30s, DEL on disconnect.
```

---

## 5. Backend Deep Dive

### 5.1 `internal/config/config.go`

Holds `Config{ DataDir, UploadDir, MaxUploadMB, ListenAddr, CookieSecret, DatabaseURL, RedisAddr, Dev }`.

- Values are bound from the environment via `caarlos0/env` struct tags, with defaults.
- **`DatabaseURL`** — PostgreSQL connection string (e.g. `postgres://gotok:gotok@localhost:5432/gotok?sslmode=disable`). Required — no SQLite fallback.
- **`RedisAddr`** — Redis address for WebSocket pub/sub + presence. Required for chat.
- `loadOrCreateSecret()` persists a random hex string to `data/cookie_secret`.

### 5.2 `internal/models/models.go`

Plain data structs with `json` tags matching the API responses:

- **`Video`** — `FilePath` is tagged `json:"-"` (never serialized).
- **`VideoWithLike`** — embeds `Video` + `Liked bool`.
- **`Like`** — straightforward.
- **`Comment`** — now includes `UserID int64` and `AvatarURL string` (resolved via JOIN) so the frontend can link to the commenter's profile.
- **`User`** — `ProviderUserID` and `PasswordHash` are tagged `json:"-"`.
- **`Conversation`** — `{ID, Type ("dm"|"group"), Title, CreatedAt}`.
- **`ConversationPreview`** — enriched for list display: other user info, last message preview, unread count, online status.
- **`Message`** — `{ID, ConversationID, SenderID, SenderName, SenderAvatar, Text, CreatedAt}`.
- **`ConversationParticipant`** — `{UserID, Name, AvatarURL, LastReadMsgID}`.

### 5.3 `internal/store/` (the data layer)

**`store.go`** — owns the `*sql.DB` connection and all existing SQL (videos, likes, comments, users, sessions). **`chat.go`** — owns chat SQL (conversations, messages, participants).

**Connection:** `sql.Open("pgx", databaseURL)` with connection pool tuning:
```go
db.SetMaxOpenConns(25)
db.SetMaxIdleConns(10)
db.SetConnMaxLifetime(5 * time.Minute)
db.SetConnMaxIdleTime(1 * time.Minute)
```

**Migrations** — managed by **golang‑migrate** with embedded SQL files:
- `000001_init.up.sql` — `videos`, `likes`, `comments`, `users`, `sessions` in PostgreSQL syntax (`BIGSERIAL`, `$N` placeholders).
- `000002_chat.up.sql` — `conversations`, `conversation_participants`, `messages`.
- Migrations run automatically on startup via `iofs.New(migrationFS, "migrations")` + `postgres.WithInstance(db)`.

**Context propagation** — every store method takes `ctx context.Context` as its first parameter and uses `QueryContext` / `ExecContext` / `BeginTx(ctx, nil)` / `QueryRowContext`.

**Key design choices:**
- **`created_at` stored as unix integer** (`BIGINT`), converted to `time.Time` on read.
- **Denormalized counters** (`likes_count`, `comments_count`) recomputed inside the same transaction.
- **`$N` placeholders** (PostgreSQL numbered parameters) instead of `?` (SQLite).
- **`RETURNING id`** instead of `LastInsertId()` (PostgreSQL doesn't support `LastInsertId()`).
- **Unique‑constraint detection** via `pgconn.PgError` error code `23505` (`pgerrcode.UniqueViolation`).
- **Keyset (cursor) pagination** by `id` — stable and fast as the dataset grows.

**Notable store methods (existing):**

| Method | What it does |
|--------|--------------|
| `CreateVideo(ctx, *Video)` | Inserts a video row, returns ID via `RETURNING id`. |
| `ListVideos(ctx, userID, afterID, limit)` | Keyset pagination with correlated `EXISTS()` for per‑user like state. |
| `ToggleLike(ctx, userID, videoID)` | Transaction: `INSERT ... ON CONFLICT DO NOTHING`, toggle, recompute count. |
| `CreateComment(ctx, userID, author, videoID, text)` | Transaction: insert comment, recompute `comments_count`. Now returns `UserID` + `AvatarURL`. |
| `ListComments(ctx, videoID, afterID, limit)` | LEFT JOIN on users for author name + avatar. |
| `CreateOrUpdateUser(ctx, provider, …)` | Upsert via `ON CONFLICT(provider, provider_user_id) DO UPDATE SET`. |
| `CreateSession(ctx, userID, token, ttl)` | Insert session row. |
| `GetUserBySession(ctx, token)` | JOIN sessions → users, checks expiry. |

**Notable store methods (chat — `chat.go`):**

| Method | What it does |
|--------|--------------|
| `GetOrCreateDMConversation(ctx, userA, userB)` | Finds existing DM between two users, or creates one in a transaction. |
| `CreateGroupConversation(ctx, title, userIDs)` | Creates a group conversation with N participants. |
| `ListConversations(ctx, userID, afterID, limit)` | Complex LATERAL JOIN: last message preview + unread count + other user info. |
| `CreateMessage(ctx, convID, senderID, text)` | Inserts message, returns it + participant IDs for broadcasting. |
| `ListMessages(ctx, convID, beforeID, limit)` | Paginated message history with sender name/avatar via JOIN. |
| `MarkRead(ctx, convID, userID, msgID)` | Advances `last_read_msg_id` via `GREATEST()` (never regresses). |
| `IsParticipant(ctx, convID, userID)` | Authorization check for conversation access. |
| `GetParticipants(ctx, convID)` | Returns all participant user IDs (for WebSocket broadcasting). |

### 5.4 `internal/chat/` (the real‑time layer)

**`hub.go`** — Per‑instance WebSocket connection manager.

- `Hub` struct holds a `sync.RWMutex`‑protected map: `clients map[int64]map[*Client]struct{}` (userID → set of connections, supporting multi‑tab).
- `Run(ctx)` — event loop on `select { register / unregister / ctx.Done() }`.
- `Register(c)` / `Unregister(c)` — channel‑based registration (only the hub goroutine touches the map; the mutex protects reads from the heartbeat goroutine).
- On first connection for a user: `broker.Subscribe(ctx, userID, onMsg)` + `broker.SetPresence(ctx, userID, true)`.
- On last disconnection: `broker.Unsubscribe(ctx, userID)` + `broker.SetPresence(ctx, userID, false)`.
- `HandleMessage(ctx, userID, env)` — persists via store, publishes to all participants' Redis channels (including the sender for multi‑tab echo).
- `HandleRead(ctx, userID, env)` — marks read + publishes read receipts to other participants.
- `StartPresenceHeartbeat(ctx)` — uses `time.NewTimer` (not `time.After`) to refresh presence TTL every 30s.
- `closeAll()` — called on shutdown, closes all client send channels.
- Implements `StoreInterface` (subset of store methods) for testability.
- `Broker` interface: `Publish`, `Subscribe`, `Unsubscribe`, `SetPresence`, `IsOnline` — compile‑time check: `var _ Broker = (*RedisBroker)(nil)`.

**`broker.go`** — Redis Pub/Sub implementation of `Broker`.

- **Per‑user channel**: `chat:user:<userID>` — each instance subscribes on first local connection for that user, unsubscribes on last.
- **Subscribe goroutine**: reads from `sub.Channel()` and calls the `onMsg` callback → `hub.deliverLocal`.
- **Presence**: `SET presence:<userID> 1 EX 120` on connect; `DEL` on disconnect; `EXISTS` to check.
- Tracks subscriptions in a `sync.Mutex`‑protected map so duplicate subscribes are no‑ops.

**`ws.go`** — WebSocket handler with coder/websocket.

- `ServeWS(hub, lg, w, r, userID)` — accepts the WS upgrade, creates a `Client`, registers with the hub.
- **Read pump** (main goroutine): reads JSON `Envelope`s, dispatches to `hub.HandleMessage` or `hub.HandleRead` with a 5s timeout per message.
- **Write pump** (separate goroutine): reads from `client.Send()` channel, writes to the WS with a 10s write deadline.
- Limits: `readLimit = 4 KiB` per message, `readTimeout = 60s`, `writeTimeout = 10s`.
- On exit: `hub.Unregister(client)` → close WS.

### 5.5 `internal/middleware/auth.go`

**`Auth(st)`** — runs on *every* request: reads the `session` cookie, looks up the user via `store.GetUserBySession(ctx, token)`, stashes `*models.User` in `gin.Context`. Never blocks.

**`RequireAuth()`** — applied to like, comment‑create, upload, profile‑edit, and all chat routes. Returns **401** when no user.

### 5.6 `internal/handlers/`

**`handlers.go`** — `Handlers` struct holds `*config.Config`, `*store.Store`, `*chat.Hub`, `*zap.Logger`.

- **`feed.go`** — `FeedPage` + `ListVideos` (infinite scroll API).
- **`upload.go`** — `UploadPage` + `Upload` (multipart validation/storage).
- **`video.go`** — `ServeFile` (Range‑aware streaming).
- **`like.go`** — `ToggleLike` + `View`.
- **`comment.go`** — `ListComments` + `CreateComment`. Comments now return `user_id` + `avatar_url` so the frontend can render clickable profile links.
- **`auth.go`** — Login/Logout/Register/Me + session management.
- **`profile.go`** — `ProfilePage` (now passes `IsLoggedIn` flag for the Message button) + `EditProfile` + `ListVideosByUser` + `ListLikedVideos`.
- **`chat.go`** — Full chat REST API + WebSocket upgrade handler:
  - `ChatPage` — renders the chat UI.
  - `ListConversations` — paginated conversation list with unread counts + presence.
  - `CreateConversation` — creates DM or group conversation.
  - `ListMessages` — paginated message history.
  - `SendMessage` — REST fallback (primary path is WebSocket).
  - `MarkConversationRead` — advances the read cursor.
  - `GetPresence` — checks online status via Redis.
  - `HandleWebSocket` — upgrades to WS (behind `RequireAuth`).

---

## 6. API Reference

| Method | Path | Handler | Auth | Purpose |
|--------|------|---------|------|---------|
| GET  | `/` | (inline) | – | Redirects to `/feed`. |
| GET  | `/feed` | `FeedPage` | – | Feed HTML shell. |
| GET  | `/upload` | `UploadPage` | – | Upload form HTML. |
| GET  | `/chat` | `ChatPage` | – | Chat UI HTML. |
| GET  | `/u/:id` | `ProfilePage` | – | User profile HTML. |
| GET  | `/uploads/:filename` | `ServeFile` | – | Streams a video (Range‑aware). |
| GET  | `/static/*` | gin.Static | – | CSS/JS assets. |
| GET  | `/login` | `LoginPage` | – | Login page. |
| POST | `/logout` | `Logout` | – | Ends session → redirect `/feed`. |
| POST | `/auth/demo` | `LoginDemo` | – | Demo login (creates user + session). |
| POST | `/auth/login` | `Login` | – | Email/password login. |
| POST | `/auth/register` | `Register` | – | Email/password registration. |
| POST | `/auth/google` | `LoginGoogle` | – | **501 placeholder**. |
| POST | `/auth/facebook` | `LoginFacebook` | – | **501 placeholder**. |
| **GET** | **`/ws`** | **`HandleWebSocket`** | **🔒 login** | **WebSocket upgrade for real‑time chat.** |
| GET  | `/api/videos` | `ListVideos` | – | Page of videos (`cursor`, `limit`). |
| GET  | `/api/users/:id/videos` | `ListVideosByUser` | – | User's videos. |
| GET  | `/api/users/:id/liked` | `ListLikedVideos` | – | User's liked videos. |
| GET  | `/api/users/:id/presence` | `GetPresence` | – | Online status (`{online: bool}`). |
| GET  | `/api/me` | `Me` | – | Current user or `null`. |
| POST | `/api/videos/:id/view` | `View` | – | Increment views. |
| POST | `/api/videos/:id/like` | `ToggleLike` | **🔒 login** | Toggle like → `{liked, count}`. |
| GET  | `/api/videos/:id/comments` | `ListComments` | – | Page of comments (now includes `user_id`, `avatar_url`). |
| POST | `/api/videos/:id/comments` | `CreateComment` | **🔒 login** | Add comment → `{comment, count}`. |
| POST | `/api/upload` | `Upload` | **🔒 login** | Multipart upload. |
| POST | `/api/profile` | `EditProfile` | **🔒 login** | Update name/bio/avatar. |
| **GET** | **`/api/conversations`** | **`ListConversations`** | **🔒 login** | **Conversation list (unread counts, presence, last message).** |
| **POST** | **`/api/conversations`** | **`CreateConversation`** | **🔒 login** | **Create DM (`{user_id}`) or group (`{user_ids, title}`).** |
| **GET** | **`/api/conversations/:id/messages`** | **`ListMessages`** | **🔒 login** | **Message history (paginated).** |
| **POST** | **`/api/conversations/:id/messages`** | **`SendMessage`** | **🔒 login** | **Send message (REST fallback for WS).** |
| **POST** | **`/api/conversations/:id/read`** | **`MarkConversationRead`** | **🔒 login** | **Mark conversation as read.** |

### WebSocket Protocol (JSON over `/ws`)

**Client → Server:**
```json
{"type":"message","conversation_id":123,"text":"hello"}
{"type":"read","conversation_id":123,"message_id":456}
```

**Server → Client:**
```json
{"type":"message","id":789,"conversation_id":123,"sender_id":1,"sender_name":"Alice","sender_avatar":"/uploads/a.jpg","text":"hello","created_at":1719500000}
{"type":"read_receipt","conversation_id":123,"user_id":2,"message_id":789}
```

**Pagination contract:** request `cursor=<id>&limit=<1..50>` (videos/comments) or `before=<id>&limit=<1..100>` (messages); response includes `next` = the last item's id (or `0`).

---

## 7. Frontend Deep Dive

No build step. Static files served by Gin.

### 7.1 Templates (`web/templates/`)

- **`layout.html`** — header/footer partials + sidebar nav (Feed, Upload, Chat, user profile link, logout).
- **`feed.html`** — feed shell; loads `feed.js`.
- **`upload.html`** — upload form; loads `upload.js`.
- **`profile.html`** — profile page with avatar, bio, video/liked tabs, edit modal, and **Message button** (shown to logged‑in non‑owners); loads `profile.js`.
- **`chat.html`** — two‑panel chat layout (conversation list + message thread); loads `chat.js`.
- **`login.html`** — login page; loads `login.js`.

### 7.2 `web/static/js/feed.js`

Feed UX + comment modal. Key changes:
- **`renderComment(c)`** — comment avatars and author names are now **clickable links** to `/u/:user_id` (matching the feed's `authorRow()` pattern). Real avatar images are shown when `avatar_url` is available; otherwise a letter‑badge fallback.
- Comments API now returns `user_id` and `avatar_url` per comment.

### 7.3 `web/static/js/profile.js`

- Tabbed video grid (Videos / Liked) with infinite scroll.
- Edit profile modal (name, bio, avatar upload).
- **Message button** (`#messageBtn`) — visible to logged‑in non‑owners. On click: `POST /api/conversations {user_id: N}` → redirect to `/chat?c=<conversation_id>`.

### 7.4 `web/static/js/chat.js` (new ~250 lines)

Full WebSocket chat client:
- **Auto‑reconnecting WebSocket** — connects to `/ws`, reconnects after 3s on close.
- **Conversation list** — loads via `GET /api/conversations`, renders avatar/name/last‑message/unread‑badge/online‑dot, infinite scroll.
- **Message thread** — loads history via `GET /api/conversations/:id/messages`, renders left/right aligned bubbles (self vs others), infinite scroll for older messages.
- **Send** — primary path via WebSocket; REST fallback if WS is down.
- **Read receipts** — marks conversation read on open and on new message; displays "Seen" when a read receipt arrives.
- **Presence** — updates online dot/status from WS `presence` events and initial conversation list.
- **URL param `?c=<id>`** — opens a specific conversation directly (used by the profile Message button).
- **Mobile responsive** — single‑panel with back button; `.chat-app.thread-open` toggles list/thread.

### 7.5 `web/static/css/style.css`

- Black, full‑screen, mobile‑first.
- `[hidden] { display: none !important; }` — ensures the HTML `hidden` attribute works even on elements with `display: flex`.
- Chat styles: conversation list, message bubbles, online dots, unread badges, message input bar, responsive two‑panel → single‑panel on mobile.
- Accent color: `#ff3b5c`.

---

## 8. Data Flow: End‑to‑End Examples

### Uploading a video
```
User picks file → upload.js POST /api/upload (multipart)
  → handlers.Upload:
      1. validate MIME/extension + size (≤200MB)
      2. title = form title OR filename stem
      3. stored name = "<unixnano>-<randID><ext>"
      4. c.SaveUploadedFile → data/uploads/<stored>
      5. store.CreateVideo(ctx, *Video) → INSERT ... RETURNING id
      6. on DB error → os.Remove(file) (cleanup)
  → returns {id, filename, title}
upload.js → redirect to /feed
```

### Sending a chat message
```
User types in chat.js → wsSend({type:"message", conversation_id, text})
  → chat/ws.go ServeWS read pump:
      hub.HandleMessage(ctx, senderID, env)
        1. store.IsParticipant → verify access
        2. store.CreateMessage → INSERT ... RETURNING id (PostgreSQL)
        3. broker.Publish(participantID, payload) for each participant → Redis pub/sub
  → This instance's Redis subscriber: hub.deliverLocal(senderID, payload) → sender's WS (echo for multi‑tab)
  → Other instance's Redis subscriber: hub.deliverLocal(recipientID, payload) → recipient's WS
  → chat.js handleWSMessage: receiveMessage(data) → render bubble
```

### Starting a conversation from a profile
```
Profile page → click "💬 Message"
  → profile.js: POST /api/conversations {user_id: N}
      → handlers.CreateConversation:
          store.GetOrCreateDMConversation(ctx, currentUserID, targetUserID)
      → returns {conversation: {id: 42}}
  → redirect to /chat?c=42
  → chat.js init: loadConversations → openConversation({id:42})
```

---

## 9. Key Design Decisions & Conventions

1. **PostgreSQL replaces SQLite.** The single‑writer limitation (`SetMaxOpenConns(1)`) is incompatible with horizontal scaling. PostgreSQL's MVCC allows concurrent writers across instances. Connection pool tuned to 25 open / 10 idle.
2. **Redis pub/sub for cross‑instance WebSocket delivery.** Each instance subscribes to `chat:user:<userID>` channels for its locally connected users. Messages are published to each recipient's channel. This avoids flooding all instances with every message.
3. **Redis presence keys with TTL.** `SET presence:<id> EX 120` on connect, refreshed every 30s by the hub heartbeat (using `time.NewTimer`, not `time.After`). Automatic expiry handles crashed instances.
4. **Context propagation everywhere.** All store methods take `ctx context.Context` as the first parameter and use `*Context` SQL variants. This enables request cancellation, timeouts, and graceful shutdown.
5. **golang‑migrate for schema management.** Replaces hand‑rolled `addColumnIfMissing` / `pragma_table_info`. Versioned `.up.sql` / `.down.sql` files embedded via `//go:embed`, run automatically on startup.
6. **Graceful shutdown.** `signal.NotifyContext` in `main.go` → `http.Server.Shutdown()` + hub `closeAll()` + Redis client close. Goroutines respect `ctx.Done()`.
7. **Denormalized counts** kept consistent via transactions.
8. **Server‑rendered shells, client‑rendered data.**
9. **`filepath.Base()` on served filenames** — prevents path traversal.
10. **Manual `escapeHtml`** on the frontend for all user‑supplied text.
11. DO NOT add unnecessary comments.

---

## 10. Running & Developing

### With Docker Compose (recommended — includes PostgreSQL + Redis + 2 instances)

```bash
cp example.env .env   # adjust DATABASE_URL / REDIS_ADDR if needed
make up               # docker compose up --build
                      # → gotok-1 on :8080, gotok-2 on :8081
                      # → PostgreSQL on :5432, Redis on :6379
make down             # stop all services
```

### Local development (requires running PostgreSQL + Redis)

```bash
make run        # go run ./cmd/gotok → http://localhost:8080
make build      # → ./gotok
make vet        # go vet ./...
make fmt        # gofmt -s -w .
make tidy       # go mod tidy
make test       # go test ./...
make test-race  # go test -race ./...
make clean      # rm -f gotok
make reset      # rm -f gotok && rm -rf data
```

**Environment variables** (see `example.env`):

| Variable | Default | Purpose |
|----------|---------|---------|
| `GOTOK_DATABASE_URL` | `postgres://gotok:gotok@localhost:5432/gotok?sslmode=disable` | PostgreSQL DSN. |
| `GOTOK_REDIS_ADDR` | `localhost:6379` | Redis for pub/sub + presence. |
| `GOTOK_LISTEN_ADDR` | `:8080` | HTTP listen address. |
| `GOTOK_DATA_DIR` | `data` | Data directory (uploads, cookie secret). |
| `GOTOK_MAX_UPLOAD_MB` | `200` | Max upload size. |
| `GOTOK_DEV` | `false` | Enables Swagger UI at `/swagger/index.html`. |

### Horizontal scaling test

`docker-compose.yml` runs **two GoTok instances** (`:8080` and `:8081`) sharing one PostgreSQL + one Redis. Open a browser tab on `:8080` and another on `:8081`, log in as different users, and send messages — they route cross‑instance via Redis pub/sub.

---

## 11. Where to Make Common Changes

| You want to… | Touch this |
|--------------|------------|
| Add a new API endpoint | `internal/app/app.go` (register route) + new method in `internal/handlers/<feature>.go` + store method in `internal/store/store.go` or `chat.go`. |
| Add a DB migration | Create `internal/store/migrations/000NNN_<name>.up.sql` + `.down.sql`. golang‑migrate picks it up automatically. |
| Change the upload size limit / port | `internal/config/config.go` or env vars. |
| Add a new accepted video format | the `allowedVideo` and `extToMime` maps in `internal/handlers/upload.go`. |
| Change the feed page size | the `20` in `feed.js` (`loadPage`) and the default in store/handlers. |
| Change look & feel | `web/static/css/style.css`. |
| Add a chat event type | Extend `Envelope` in `internal/chat/hub.go` + handle in `chat/ws.go` read pump + `chat.js` `handleWSMessage`. |
| Add WebSocket middleware | `internal/app/app.go` route registration for `/ws`. |
| Implement Google/Facebook SSO | `handlers/LoginGoogle`/`LoginFacebook` (currently return 501). |
| Sign the session cookie | use `cfg.CookieSecret` with Gin's secure cookie mechanism. |

---

## 12. Gotchas, Limitations & Next Steps

- **Chat schema needs production review.** The `conversations`, `conversation_participants`, and `messages` tables were designed for this feature but may need index tuning, partitioning, or constraint adjustments for high‑volume production workloads.
- **No tests yet** — `go test ./...` passes vacuously. High‑value targets: `store` (especially `ToggleLike`, `GetOrCreateDMConversation`, pagination), `chat/hub.go` (goroutine leak detection via `go.uber.org/goleak`), `chat/broker.go` (Redis pub/sub round‑trip).
- **SSO is stubbed.** Google/Facebook endpoints return 501.
- **`cookie_secret` is generated but not used** to sign the `session` cookie; the token is an unsigned random string.
- **Uploads are local disk.** With multiple instances, each instance has its own `data/uploads/` (separate Docker volumes in `docker-compose.yml`). For production, use shared object storage (S3) so all instances see the same uploads.
- **No content moderation on upload.**
- **View counting is client‑initiated** and deduped only in‑memory per page load.
- **Comments have no edit/delete, no threading, no replies.**
- **`time` stored as unix seconds** loses sub‑second precision.
- **WebSocket origin check is disabled** (`InsecureSkipVerify: true`) — the session cookie is the auth mechanism. Tighten for production if needed.

---

### TL;DR

GoTok = **Gin + PostgreSQL + Redis + WebSocket** for a TikTok‑style video app with real‑time chat. Session‑based login (SSO stubbed, demo + email/password live), local‑disk uploads, keyset‑paginated JSON feed, scroll‑snapping vertical player, transactional like/comment counters, 1‑on‑1 + group chat with read receipts and presence, horizontally scalable via Redis pub/sub. Graceful shutdown via `signal.NotifyContext`. Schema migrations via golang‑migrate with embedded SQL. Start in `cmd/gotok/main.go` for wiring, `internal/app/app.go` for routes + startup/shutdown, `internal/store/` for all data logic, `internal/chat/` for the WebSocket hub + Redis broker, and `web/static/js/` for all UX.
