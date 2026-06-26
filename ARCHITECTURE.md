# GoTok — Knowledge Transfer / Architecture Guide

A complete walkthrough of how the **GoTok** system works. Read this top‑to‑bottom and you should be able to navigate, modify, and extend the codebase confidently.

---

## 1. What is GoTok?

GoTok is a **single‑binary, TikTok‑style vertical‑video web app** written in Go.

- **Browsing is anonymous; interactions require login.** Every visitor gets a stable `cid` cookie, and can browse the feed and read comments freely. **Liking, commenting, and uploading are gated behind a session login** (SSO via Google/Facebook is stubbed; a demo login is wired up so the flow is testable now).
- **Vertical "for you" feed** — full‑screen videos that auto‑play as you scroll, snap one per screen, mute/unmute on tap, like on double‑tap.
- **Upload your own videos** (mp4 / webm / mov / mkv, up to 200 MB), stored on local disk and **attributed to the uploading user**.
- **User profiles** (`/u/:id`) — every video records its uploader; a profile page lists a creator's videos in a 3‑column grid via `ListVideosByUser`.
- **Likes, comments, view counting**, and infinite scroll pagination — all backed by a SQLite database.

It is intentionally small and self‑contained: one Go binary, one SQLite file, one uploads folder.

---

## 2. Tech Stack

| Layer | Technology | Notes |
|-------|------------|-------|
| Language | **Go 1.25.6** | `cmd/gotok` entry point + `internal/*` packages. |
| Web framework | **Gin** (`github.com/gin-gonic/gin v1.12.0`) | HTTP routing, middleware, HTML templates, multipart upload. |
| Database | **SQLite** via `modernc.org/sqlite` | Pure‑Go driver (no CGO required). |
| Templating | Gin's built‑in `html/template` loader | `LoadHTMLGlob("web/templates/*")`. |
| Frontend | **Vanilla HTML/CSS/JS** (no framework) | `fetch` + DOM manipulation, `IntersectionObserver`, CSS scroll‑snap. |
| Storage | Local filesystem | Uploaded videos live under `data/uploads/`. |

Key indirect deps worth knowing: `quic-go` (HTTP/3 capable), `google/uuid`, `go-playground/validator`. These come in transitively via Gin; you rarely touch them directly.

---

## 3. Project Structure

```
gotok/
├── cmd/
│   └── gotok/main.go          # Thin entry point: wires config → store → app.Run()
├── go.mod / go.sum            # Module `github.com/hokkung/gotok`, Go 1.25.6
├── Makefile                   # Dev tasks: run, build, serve, vet, fmt, tidy, test, clean, reset
├── gotok                      # Compiled binary (build artifact)
│
├── internal/                  # All non‑main Go code (Go's "internal" import protection)
│   ├── app/app.go             # Gin engine setup + route registration (called by main)
│   ├── config/config.go       # App config + on‑disk cookie‑secret bootstrap
│   ├── models/models.go       # Plain structs: Video, VideoWithLike, Like, Comment
│   ├── store/store.go         # SQLite layer: open, migrate, all SQL queries
│   ├── middleware/             # Gin middleware: auth, logging, recovery
│   └── handlers/              # HTTP handlers (one concern per file)
│       ├── handlers.go        #   Handlers struct + constructor (dependency holder)
│       ├── helpers.go         #   randID() helper for unique filenames
│       ├── feed.go            #   FeedPage + ListVideos (infinite scroll API)
│       ├── upload.go          #   UploadPage + Upload (multipart validation/storage)
│       ├── video.go           #   ServeFile (streams video w/ Range support)
│       ├── like.go            #   ToggleLike + View
│       ├── comment.go         #   ListComments + CreateComment
│       ├── auth.go            #   Login/Logout/Me + session management
│       └── profile.go         #   ProfilePage + EditProfile
│
├── web/                       # Frontend assets
│   ├── templates/             # Gin HTML templates (header/footer partials + pages)
│   │   ├── layout.html        #   {{define "header"}} / {{define "footer"}}
│   │   ├── feed.html          #   The feed shell; loads feed.js
│   │   └── upload.html        #   The upload form; loads upload.js
│   └── static/
│       ├── css/style.css      # All styling (feed, upload, comment sheet)
│       └── js/
│           ├── feed.js        # Feed rendering, gestures, likes, comments modal
│           └── upload.js      # Drag‑and‑drop + form submit
│
└── data/                      # Runtime data (created automatically; do NOT commit)
    ├── app.db                 # SQLite database
    ├── app.db-wal / -shm      # SQLite WAL files (live alongside the db)
    ├── cookie_secret          # 32‑byte hex secret for stable client ids
    └── uploads/               # Uploaded video files
```

> **Convention:** business logic lives in `internal/`, split by *concern* (app / config / models / store / middleware / handlers). Handlers are further split by *feature* (feed, upload, like, comment, video). Follow this pattern when adding new features.

---

## 4. High‑Level Architecture & Request Flow

### Startup (`cmd/gotok/main.go` → `internal/app/app.go`)

```
config.Load() ──► logger.New() ──► store.New(cfg.DBPath, lg) ──► app.Run(): gin.New()+zap middleware ──► register routes ──► r.Run(:8080)
```

1. **`config.Load()`** — ensures `data/` and `data/uploads/` exist; generates and persists a 32‑byte `cookie_secret` on first run (so session tokens survive restarts). Returns hardcoded defaults: `:8080`, 200 MB upload cap, paths under `data/`.
2. **`store.New(dbPath)`** — opens SQLite in WAL mode with a 5 s busy timeout, sets **`SetMaxOpenConns(1)`** (single writer avoids "database is locked"), runs migrations.
3. **Gin setup** — `gin.New()` + zap-backed `GinLogger`/`GinRecovery` middleware (access logs and panics flow through `go.uber.org/zap` rather than the std logger), sets `MaxMultipartMemory = 32 MiB` (larger uploads spill to temp files), registers the `Auth()` middleware (loads the logged-in user from the `session` cookie on every request), loads HTML templates, mounts `/static`.
4. **Routes** are registered (see §6), then `r.Run(":8080")` blocks and serves.

### A typical feed request

```
Browser (session cookie) ──► GET /api/videos?cursor=N&limit=20
   │
   ├─ middleware.Auth(): read session cookie → load user (nil if anonymous)
   ├─ handlers.ListVideos(): read userID (0 = anon) + cursor + limit
   │     └─ store.ListVideos(userID, cursor, limit) → SQL EXISTS() for per‑user like state
   └─ responds JSON { videos: [...], next: <last id or 0> }
```

The `next` cursor is the **last item's ID**. The client appends it to the next request; the store returns rows with `id < cursor` (newest‑first, keyset pagination).

---

## 5. Backend Deep Dive

### 5.1 `internal/config/config.go`

Holds `Config{ DataDir, UploadDir, DBPath, MaxUploadMB, ListenAddr, CookieSecret }`.

- All values are **hardcoded defaults** (no env vars / flags yet). If you want to make the port or upload limit configurable, this is the place.
- `loadOrCreateSecret()` persists a random hex string to `data/cookie_secret` (mode `0600`). The secret currently isn't used to *sign* the `session` cookie (the cookie is just a random token), but it's there for future signed‑cookie schemes.

### 5.2 `internal/models/models.go`

Plain data structs with `json` tags matching the API responses:

- **`Video`** — `FilePath` is tagged `json:"-"` so it's **never** serialized to clients (server‑only absolute path).
- **`VideoWithLike`** — embeds `Video` and adds `Liked bool` (per‑requester like state). This is what `ListVideos` and `GetVideo` return.
- **`Like`**, **`Comment`** — straightforward. `Comment.Author` is a *derived* display name (e.g. `guest_a1b2c3`), not stored — computed by `authorName(clientID)` in the store.

### 5.3 `internal/store/store.go` (the data layer)

This is the most important file. It owns the `*sql.DB` and **all** SQL.

**Connection:** `sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")` + `SetMaxOpenConns(1)`.

**Schema (auto‑created by `migrate()`):**

```sql
videos(
  id INTEGER PK AUTOINCREMENT,
  title TEXT NOT NULL DEFAULT '',
  filename TEXT NOT NULL,          -- on-disk filename, e.g. 1718000000000000000-a1b2c3.mp4
  filepath TEXT NOT NULL,          -- absolute path on disk (never sent to clients)
  mime_type TEXT NOT NULL,
  size INTEGER NOT NULL,
  likes_count INTEGER NOT NULL DEFAULT 0,     -- denormalized cache
  comments_count INTEGER NOT NULL DEFAULT 0,  -- denormalized cache
  views INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL                  -- unix timestamp (seconds)
);

likes(
  id INTEGER PK AUTOINCREMENT,
  user_id INTEGER NOT NULL,         -- FK→users.id (the logged-in user)
  video_id INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(user_id, video_id)         -- one like per user per video
);

comments(
  id INTEGER PK AUTOINCREMENT,
  user_id INTEGER NOT NULL,         -- FK→users.id
  video_id INTEGER NOT NULL,
  text TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT 0
);

-- Indexes
idx_likes_video        ON likes(video_id)
idx_comments_video     ON comments(video_id, id DESC)
idx_videos_created     ON videos(created_at DESC)
```

**Key design choices:**

- **`created_at` is stored as a unix integer**, then converted to `time.Time` on read (`time.Unix(created, 0)`). Keeps storage compact and timezone‑free.
- **Denormalized counters** (`likes_count`, `comments_count`) are recomputed inside the same transaction that mutates `likes`/`comments`, so they're always consistent.
- **Migrations are lightweight and idempotent**: `CREATE TABLE IF NOT EXISTS` + an `addColumnIfMissing()` helper + guarded table rebuilds. This is how `comments_count` was backfilled, and how the `cid`→`user_id` switch rebuilt the `likes`/`comments` tables in place (dropping orphaned anonymous data and resetting video counts).

**Notable store methods:**

| Method | What it does |
|--------|--------------|
| `CreateVideo(*Video) (id, err)` | Inserts a video row. |
| `ListVideos(userID, afterID, limit)` | Keyset pagination (`id < afterID`), newest first, with a correlated `EXISTS()` subquery to compute `liked` **for the requesting user** in one query. `userID=0` means anonymous (liked is always false). Clamps `limit` to 1–50 (default 20). |
| `GetVideo(userID, id)` | Single video + user's like state. Used as an existence check by `ToggleLike` and `CreateComment`. |
| `ToggleLike(userID, videoID)` | **Transaction**: `INSERT ... ON CONFLICT(user_id, video_id) DO NOTHING`; if no row was affected (already liked) it `DELETE`s; then recomputes `likes_count`. Returns `(liked bool, count int64)`. |
| `CreateComment(userID, author, videoID, text)` | **Transaction**: insert comment, recompute `comments_count`, return the built `Comment` (with `Author` = the user's display name) + new count. |
| `ListComments(videoID, afterID, limit)` | Keyset pagination, newest first. Author name resolved via LEFT JOIN on `users`. |
| `IncrementViews(id)` | `UPDATE videos SET views = views + 1`. Fire‑and‑forget (no error returned). |

> **Why one `EXISTS` subquery instead of a JOIN?** It avoids row multiplication and lets the feed query stay simple while still returning per‑viewer like state.

### 5.4 `internal/middleware/auth.go`

**`Auth(st)`** — runs on *every* request: reads the `session` cookie, looks up the user via `store.GetUserBySession(token)`, and stashes the `*models.User` in `gin.Context` (nil when anonymous or expired). Never blocks a request.

**`RequireAuth()`** — applied *only* to the like and comment‑create routes. Aborts with **401** when no user is in the context. The frontend treats 401 as "redirect to `/login?next=<url>`".

**`UserFromContext(c)`** — typed accessor returning `*models.User` (or nil).

> Browsing is anonymous; only liking/commenting require a session. The `session` cookie is an opaque random token (32 bytes, hex). The legacy anonymous `cid` cookie has been removed entirely.

### 5.5 `internal/handlers/`

**`handlers.go`** — `Handlers` struct holds `*config.Config` + `*store.Store`; `New(cfg, st)` is the constructor. Every handler is a method on `*Handlers`, so they all share these dependencies.

Each file maps to a feature:

- **`feed.go`** — `FeedPage` renders `feed.html`; `ListVideos` handles `GET /api/videos?cursor=&limit=`, reads `userID` from context (0 = anonymous), calls `store.ListVideos`, returns `{videos, next}`.
- **`upload.go`** — defines `allowedVideo` (MIME → extension) and `extToMime` (reverse) maps for **mp4/webm/mov/mkv**. `UploadPage` renders the form; `Upload` validates MIME (falls back to extension), enforces size cap, derives a title from the filename if none given, builds a unique name `<unixnano>-<randID(6)><ext>`, saves via `c.SaveUploadedFile`, inserts the DB row, and **deletes the file on disk if the DB insert fails** (cleanup). Returns `{id, filename, title}`.
- **`video.go`** — `ServeFile` (`GET /uploads/:filename`) calls `filepath.Base()` (path‑traversal defense) then `c.File()`, which uses `http.ServeFile` and therefore **honors HTTP `Range` requests** — this is what lets the browser seek/scrub videos.
- **`like.go`** — `ToggleLike` parses id, verifies the video exists (`GetVideo`), toggles, returns `{liked, count}`. `View` bumps the view counter (the client fires this once per video when it first scrolls into view).
- **`comment.go`** — `maxCommentLen = 500` (truncated by **rune** count, Unicode‑safe). `ListComments` returns `{comments, next}`; `CreateComment` validates non‑empty, truncates, returns `{comment, count}`.
- **`helpers.go`** — `randID(n)` returns `2n` hex chars from `crypto/rand`. Used only for upload filenames.

**Error‑handling convention:** handlers return small JSON errors like `gin.H{"error": "..."}` with an appropriate status code (400 / 404 / 413 / 500). The frontend reads `data.error`.

---

## 6. API Reference

| Method | Path | Handler | Auth | Purpose |
|--------|------|---------|------|---------|
| GET  | `/` | (inline) | – | Redirects to `/feed`. |
| GET  | `/feed` | `FeedPage` | – | Feed HTML shell. |
| GET  | `/upload` | `UploadPage` | – | Upload form HTML. |
| GET  | `/uploads/:filename` | `ServeFile` | – | Streams a video (Range‑aware). |
| GET  | `/static/*` | gin.Static | – | CSS/JS assets. |
| GET  | `/login` | `LoginPage` | – | SSO login page (Google/Facebook buttons + demo login). |
| POST | `/logout` | `Logout` | – | Ends session → redirect `/feed`. |
| POST | `/auth/demo` | `LoginDemo` | – | Stand‑in login for testing; creates a `demo` user + session. |
| POST | `/auth/google` | `LoginGoogle` | – | **501 placeholder** — wire up OAuth here. |
| POST | `/auth/facebook` | `LoginFacebook` | – | **501 placeholder** — wire up OAuth here. |
| GET  | `/api/videos` | `ListVideos` | – | Page of videos (`cursor`, `limit`). |
| GET  | `/api/me` | `Me` | – | Current user or `null` (lets the client adapt UI). |
| POST | `/api/videos/:id/view` | `View` | – | Increment views. |
| POST | `/api/videos/:id/like` | `ToggleLike` | **🔒 login** | Toggle like → `{liked, count}`. Returns 401 if anonymous. |
| GET  | `/api/videos/:id/comments` | `ListComments` | – | Page of comments (`cursor`, `limit`). |
| POST | `/api/videos/:id/comments` | `CreateComment` | **🔒 login** | Add comment (form `text`) → `{comment, count}`. Returns 401 if anonymous. |
| POST | `/api/upload` | `Upload` | – | Multipart upload (`file`, `title`) → `{id, filename, title}`. |

**Auth model:** the `Auth` middleware loads the logged‑in user (if any) from the
`session` cookie into the context on every request. `RequireAuth()` is applied
*only* to the like and comment‑create routes — they return **401** when there's
no session. The frontend treats 401 as "redirect to `/login?next=<current‑url>`"
(via the `requireLogin()` helper in `feed.js`); after a successful login it
returns to `next`.

**Pagination contract (videos & comments):** request `cursor=<id of last item>&limit=<1..50>`; response includes `next` = the last item's id (or `0` if none). Stop paging when a page returns fewer than `limit` items or `next` is `0`.

---

## 7. Frontend Deep Dive

No build step. Just static files served by Gin.

### 7.1 Templates (`web/templates/`)

- **`layout.html`** — defines `{{define "header"}}` (doctype, nav with GoTok brand + Feed/Upload links) and `{{define "footer"}}`. Other templates `{{template "header" .}}` ... `{{template "footer" .}}` to compose a full page.
- **`feed.html`** — `<div id="feed">` + `<div id="loader">`, then loads `/static/js/feed.js`. The feed is **entirely client‑rendered** from the API.
- **`upload.html`** — dropzone `<label>`, title input, submit button; loads `/static/js/upload.js`.

### 7.2 `web/static/js/feed.js` (the big one)

Implements the whole feed UX. Key pieces:

- **`IntersectionObserver`** (thresholds `[0, 0.6, 1]`): when a card is ≥60% visible it **plays** that video and **pauses** all others; the first time a video becomes visible it fires a one‑shot `POST /api/videos/:id/view` (tracked via a `seen` Set to avoid double counting).
- **`loadPage()`** — infinite scroll. Triggered by `feed` scroll near the bottom (≤600px) as well as on load. Fetches `/api/videos?limit=20&cursor=...`, appends cards, sets `done` when a page returns <20 items.
- **`renderCard(v)`** — builds the `<section class="video-card">` with `<video muted loop playsinline>`, like/comment action buttons, title/meta overlay, and a "tap: sound · double‑tap: ❤" hint. Wires up events.
- **Gestures on the `<video>`:** single click → **mute toggle** (250 ms timer); a second click within 250 ms → **double‑tap like**. This is the TikTok interaction.
- **`toggleLike(card, id)`** — the heart‑button click handler. Optimistic `pop` animation, then `POST /like` and reconcile `liked` class + count.
- **`likeVideo(card, id, e)`** — double‑tap handler. **Always likes** (never unlikes), plays a floating heart animation (`showFloatingHeart`) at the tap coordinates, and reconciles with the server.
- **Comments modal** — a lazily‑created (`ensureModal()`) bottom sheet. `openComments(card, id)` resets state and loads page 1; `loadComments(append)` paginates on scroll; `submitComment` posts and **prepends** the new comment to the top, then updates the card's comment badge and the sheet title.
- **Helpers:** `timeAgo`, `formatCount` (K/M abbreviation), `formatDate`, `escapeHtml` (used everywhere user content is injected — **XSS defense is manual**, via this function).

### 7.3 `web/static/js/upload.js`

Drag‑and‑drop wiring + form submit. Posts `FormData` (file + title) to `/api/upload`, shows status (error/ok), and redirects to `/feed` on success.

### 7.4 `web/static/css/style.css`

- Black, full‑screen, mobile‑first.
- `.feed` uses **`scroll-snap-type: y mandatory`** + `scroll-snap-align: start` per `.video-card` → the signature one‑video‑per‑screen snapping.
- `100dvh` (dynamic viewport height) is used alongside `100vh` for mobile browser chrome handling.
- The double‑tap heart is a CSS `@keyframes tapHeart` (scale up + float up + fade).
- The comment sheet slides up via `@keyframes sheetUp`.
- Accent color throughout is `#ff3b5c` (the TikTok‑ish red/pink).

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
      5. store.CreateVideo → INSERT row
      6. on DB error → os.Remove(file) (cleanup)
  → returns {id, filename, title}
upload.js → redirect to /feed
```

### Viewing the feed
```
GET /feed → feed.html (shell)
  feed.js loadPage() → GET /api/videos?cursor=0&limit=20
    → store.ListVideos(userID, 0, 20)
        SQL: SELECT ... FROM videos ORDER BY id DESC LIMIT 20
             + EXISTS(likes for this user_id) AS liked
  renderCard() for each → <video src="/uploads/<filename>">
IntersectionObserver → plays visible video → POST /api/videos/:id/view (once)
```

### Liking (double‑tap)
```
double‑tap on video → likeVideo(card, id, e)
  → show floating heart at (x,y) (optimistic)
  → heart.classList.add('liked') (optimistic)
  → POST /api/videos/:id/like
      → store.ToggleLike(userID, id)  [TX]
          INSERT INTO likes ... ON CONFLICT DO NOTHING
          recompute likes_count
      → returns {liked, count}
  → update count, reconcile heart class
```
> Subtlety: the **heart button** calls `toggleLike` (on/off toggle), while the **double‑tap** calls `likeVideo` (only ever likes, with an early‑return `if (isLiked(card)) return;` after replaying the animation). This mirrors TikTok, where a double‑tap always likes but tapping the button can unlike.

---

## 9. Key Design Decisions & Conventions

1. **User‑keyed interactions.** Browsing (feed, comments, uploads) is anonymous. Liking and commenting are gated behind `RequireAuth()` and keyed on `users.id` — a like belongs to a logged‑in user, not an anonymous cookie. Two users sharing a browser get independent like states. Comment author names come from the `users` table via a LEFT JOIN (no more `guest_xxx` placeholders). The legacy `cid` cookie and its middleware were removed entirely.
2. **SQLite with a single writer connection** (`SetMaxOpenConns(1)`) + WAL — avoids "database is locked" under the (mild) concurrency a single‑box app sees.
3. **Denormalized counts** kept consistent via transactions — reads are cheap (the hot feed path reads the cached column instead of `COUNT(*)`).
4. **Keyset (cursor) pagination** by `id` instead of `OFFSET` — stable and fast as the dataset grows.
5. **Server‑rendered shells, client‑rendered data.** Pages are thin HTML; all dynamic content comes from JSON APIs. This keeps Go templates simple and the UX snappy.
6. **`filepath.Base()` on the served filename** — prevents path traversal (`GET /uploads/../../etc/passwd`).
7. **`json:"-"` on `FilePath`** — the on‑disk path never leaks to clients.
8. **Manual `escapeHtml`** on the frontend for any user‑supplied text (titles, comments) — there is no templating/auto‑escaping in vanilla JS.
9. **Range‑aware video serving** via `http.ServeFile` so seeking works without a custom streaming endpoint.
10. **Idempotent, additive migrations** (`IF NOT EXISTS` + `addColumnIfMissing` + guarded table rebuilds) instead of a migration framework — appropriate at this scale. The `cid`→`user_id` switch uses a guarded rebuild (`migrateLikesComments`): it detects the legacy `client_id` column and rebuilds `likes`/`comments` in place, dropping orphaned anonymous data and resetting video counts.
11. DO NOT add unnecessary comment

---

## 10. Running & Developing

Uses the **Makefile**. From the project root:

```bash
make run      # go run .        → http://localhost:8080
make build    # → ./gotok
make serve    # build then run ./gotok
make vet      # go vet ./...
make fmt      # gofmt -s -w .
make tidy     # go mod tidy
make test     # go test ./...   (note: there are currently no _test.go files)
make clean    # rm -f gotok
make reset    # rm -f gotok && rm -rf data   ← wipes DB + uploads!
```

Requirements: **Go 1.25.6** (per `go.mod`). The SQLite driver is pure Go, so **no CGO/toolchain** is needed — a plain `go build` produces `./gotok`.

First run auto‑creates `data/`, `data/uploads/`, `data/app.db`, and `data/cookie_secret`.

---

## 11. Where to Make Common Changes

| You want to… | Touch this |
|--------------|------------|
| Add a new API endpoint | `internal/app/app.go` (register route) + new method in `internal/handlers/<feature>.go` + store method in `internal/store/store.go`. |
| Change the upload size limit / port | `internal/config/config.go` (`MaxUploadMB`, `ListenAddr`). |
| Add a new accepted video format | the `allowedVideo` and `extToMime` maps in `internal/handlers/upload.go`. |
| Add a DB column / table | the `schema` string in `store.go` `migrate()` (+ `addColumnIfMissing` for backfill). |
| Change the feed page size | the `20` in `feed.js` (`loadPage`) and the default in `store.ListVideos` / `handlers.ListVideos`. |
| Change look & feel | `web/static/css/style.css`. |
| Change feed interactions (gestures) | `web/static/js/feed.js`. |
| Add a per‑user page (e.g. "liked videos") | new handler in `handlers/profile.go` + a `store.go` query reusing the `listVideosPage` keyset shape, registered in `main.go`. |
| Implement Google/Facebook SSO | `handlers/LoginGoogle`/`LoginFacebook` (currently return 501) — add OAuth redirect → callback → `store.CreateOrUpdateUser`. |
| Sign the session cookie | use `cfg.CookieSecret` (already loaded) with Gin's secure cookie mechanism. |

---

## 12. Gotchas, Limitations & Next Steps

- **No tests yet** — `go test ./...` passes vacuously. Adding table‑driven tests around `store` (especially `ToggleLike`, `ListVideos` pagination) would be high‑value.
- **SSO is stubbed.** Google/Facebook endpoints return 501 "coming soon"; only the demo login (`/auth/demo`) actually creates a session. Wire real OAuth (redirect → provider → callback → `CreateOrUpdateUser`) into `LoginGoogle`/`LoginFacebook` to enable them.
- **`cookie_secret` is generated but not used** to sign the `session` cookie; the token is an unsigned random string, so it can be forged/spoofed by a knowledgeable client. Fine for a toy, worth tightening for production.
- **Single‑process only.** SQLite + local uploads mean this won't horizontally scale as‑is. For multi‑instance you'd move uploads to object storage and either shard SQLite or move to Postgres.
- **No content moderation on upload** — uploads now require a login, but there's no size/scan/abuse protection beyond the type allow‑list and 200 MB cap. Add a reverse proxy with rate limits before exposing publicly.
- **Legacy videos have no uploader.** Videos created before ownership was added keep `user_id = 0`; they show "Unknown uploader" with no profile link.
- **View counting is client‑initiated** (`POST /view`) and deduped only in‑memory per page load (`seen` Set). Refreshing the page re‑counts. A server‑side dedupe (e.g. by user+video with a window) would be more accurate.
- **Comments have no edit/delete, no threading, no replies.**
- **`time` stored as unix seconds** loses sub‑second precision (fine here; upload filenames use `UnixNano` so they're still unique).

---

### TL;DR

GoTok = **Gin + SQLite + vanilla JS** in one Go binary. Session‑based login (SSO stubbed, demo login live), local‑disk uploads, a keyset‑paginated JSON feed rendered client‑side into a scroll‑snapping vertical player, with transactional user‑keyed like/comment counters. Start in `cmd/gotok/main.go` to see the wiring, `internal/app/app.go` for routes, drop into `internal/store/store.go` for all data logic, and `web/static/js/feed.js` for all the UX.