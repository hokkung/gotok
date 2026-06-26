# GoTok 📺

A minimal, single-binary **TikTok-style vertical-video web app** written in Go.
No frontend build step — just Gin, SQLite, and vanilla JS.

- 🔐 **Login required to like / comment / upload** — anonymous browsing is open,
  but interactions and uploads are gated behind a session. SSO (Google / Facebook)
  is stubbed; a built-in demo login lets you try the flow now.
- 📱 **Vertical feed** with full-screen auto-play, scroll-snap, tap-to-unmute, double-tap-to-like.
- ⬆️ **Upload** mp4 / webm / mov / mkv (up to 200 MB), stored on local disk and
  attributed to your account.
- 👤 **User profiles** (`/u/:id`) — every upload is owned by a user; browse a
  creator's videos in a 3-column grid.
- ❤️ **Likes, comments, view counts** backed by SQLite.
- ♾️ **Infinite scroll** via keyset cursor pagination.

---

## Quickstart

```bash
make run        # go run .  → http://localhost:8080
```

That's it. On first run the app auto-creates `data/` (SQLite DB, uploads dir, cookie secret).
Open the printed URL, go to **Upload** to add a video, then back to **Feed** to watch it.

### Other commands

```bash
make build      # compile → ./gotok
make serve      # build, then run ./gotok
make vet        # go vet ./...
make fmt        # gofmt -s -w .
make tidy       # go mod tidy
make test       # go test ./...
make clean      # rm -f gotok
make reset      # rm -f gotok && rm -rf data   (wipes DB + uploads!)
```

**Requirements:** Go 1.25.6. No CGO needed — the SQLite driver is pure Go.

---

## How it works (30-second tour)

```
Browser ──session cookie──► Gin router ──► handlers ──► store (SQLite)
                          │                              │
                          ├── /feed, /upload  (HTML)     └── data/app.db
                          ├── /uploads/:file  (video stream)
                          └── /api/*  (JSON: videos, likes, comments, upload)
```

- **`main.go`** wires `config → store → router → handlers`.
- **`internal/store/store.go`** owns the SQLite schema and all SQL (denormalized like/comment counts, keyset pagination, transactional updates).
- **`internal/middleware/auth.go`** loads the logged-in user from the `session` cookie (nil when anonymous); `RequireAuth()` gates likes/comments.
- **`internal/handlers/`** one file per feature (feed, upload, like, comment, video).
- **`web/`** server-rendered HTML shells + client-rendered data (`feed.js`, `upload.js`, `style.css`).

---

## Project structure

```
live/
├── main.go                # entry point + route registration
├── internal/
│   ├── config/            # config + cookie-secret bootstrap
│   ├── models/            # Video / Like / Comment structs
│   ├── store/             # SQLite layer (schema + queries)
│   ├── middleware/        # anonymous cid cookie
│   └── handlers/          # HTTP handlers, split by feature
├── web/
│   ├── templates/         # layout, feed, upload (Gin html/template)
│   └── static/            # css + vanilla js
└── data/                  # runtime: app.db, uploads/, cookie_secret (generated)
```

---

## Learn more

Full design rationale, data-flow diagrams, the complete API reference, and a
"where to make common changes" cheat-sheet live in **[ARCHITECTURE.md](./ARCHITECTURE.md)**.

## Status

Toy / single-instance demo. Videos are attributed to the uploading user and
browsable on per-user profile pages (`/u/:id`). Known limits before going to
production: unsigned client cookie, single-process SQLite, no tests,
client-counted views, no video thumbnails. See ARCHITECTURE.md §12 for the full list.
