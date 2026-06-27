CREATE TABLE IF NOT EXISTS videos (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT NOT NULL DEFAULT 0,
    title          TEXT NOT NULL DEFAULT '',
    filename       TEXT NOT NULL,
    filepath       TEXT NOT NULL,
    mime_type      TEXT NOT NULL,
    size           BIGINT NOT NULL,
    likes_count    BIGINT NOT NULL DEFAULT 0,
    comments_count BIGINT NOT NULL DEFAULT 0,
    views          BIGINT NOT NULL DEFAULT 0,
    created_at     BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS likes (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL,
    video_id   BIGINT NOT NULL,
    created_at BIGINT NOT NULL DEFAULT 0,
    UNIQUE(user_id, video_id)
);

CREATE TABLE IF NOT EXISTS comments (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL,
    video_id   BIGINT NOT NULL,
    text       TEXT NOT NULL,
    created_at BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS users (
    id               BIGSERIAL PRIMARY KEY,
    provider         TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    name             TEXT NOT NULL DEFAULT '',
    email            TEXT NOT NULL DEFAULT '',
    avatar_url       TEXT NOT NULL DEFAULT '',
    bio              TEXT NOT NULL DEFAULT '',
    password_hash    TEXT NOT NULL DEFAULT '',
    created_at       BIGINT NOT NULL,
    UNIQUE(provider, provider_user_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_likes_video   ON likes(video_id);
CREATE INDEX IF NOT EXISTS idx_likes_user    ON likes(user_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_comments_video ON comments(video_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_videos_created ON videos(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_user  ON sessions(user_id);
