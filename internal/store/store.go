package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"live/internal/models"
)

// Store wraps the database connection and provides data access.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database and initializes the schema.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// Single writer connection avoids SQLite "database is locked" errors.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	// created_at is stored as a unix timestamp (int) so reads map cleanly to time.Time.
	const schema = `
	CREATE TABLE IF NOT EXISTS videos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL DEFAULT '',
		filename TEXT NOT NULL,
		filepath TEXT NOT NULL,
		mime_type TEXT NOT NULL,
		size INTEGER NOT NULL,
		likes_count INTEGER NOT NULL DEFAULT 0,
		comments_count INTEGER NOT NULL DEFAULT 0,
		views INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS likes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		video_id INTEGER NOT NULL,
		created_at INTEGER NOT NULL DEFAULT 0,
		UNIQUE(user_id, video_id)
	);
	CREATE TABLE IF NOT EXISTS comments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		video_id INTEGER NOT NULL,
		text TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_likes_video ON likes(video_id);
	CREATE INDEX IF NOT EXISTS idx_comments_video ON comments(video_id, id DESC);
	CREATE INDEX IF NOT EXISTS idx_videos_created ON videos(created_at DESC);
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider TEXT NOT NULL,
		provider_user_id TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		email TEXT NOT NULL DEFAULT '',
		avatar_url TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		UNIQUE(provider, provider_user_id)
	);
	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Backfill comments_count on databases created before this column existed.
	if err := s.addColumnIfMissing("videos", "comments_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	// Migrate likes/comments from anonymous client_id keying to user_id keying.
	return s.migrateLikesComments()
}

// hasColumn reports whether a column exists on a table.
func (s *Store) hasColumn(table, col string) bool {
	var name string
	err := s.db.QueryRow("SELECT name FROM pragma_table_info(?) WHERE name = ?", table, col).Scan(&name)
	return err == nil
}

// migrateLikesComments rebuilds the likes and comments tables to key on user_id
// instead of the legacy anonymous client_id. Old rows cannot be attributed to a
// user (there was no cid→user mapping), so they are dropped and video counts are
// reset. Runs once on databases that predate user-keyed interactions; a no-op on
// fresh databases (which are created with user_id directly).
func (s *Store) migrateLikesComments() error {
	if !s.hasColumn("likes", "client_id") {
		return nil // already on user_id schema
	}
	_, err := s.db.Exec(`
		CREATE TABLE likes_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			video_id INTEGER NOT NULL,
			created_at INTEGER NOT NULL DEFAULT 0,
			UNIQUE(user_id, video_id)
		);
		DROP TABLE likes;
		ALTER TABLE likes_new RENAME TO likes;
		CREATE INDEX IF NOT EXISTS idx_likes_video ON likes(video_id);

		CREATE TABLE comments_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			video_id INTEGER NOT NULL,
			text TEXT NOT NULL,
			created_at INTEGER NOT NULL DEFAULT 0
		);
		DROP TABLE comments;
		ALTER TABLE comments_new RENAME TO comments;
		CREATE INDEX IF NOT EXISTS idx_comments_video ON comments(video_id, id DESC);

		UPDATE videos SET likes_count = 0, comments_count = 0;
	`)
	return err
}

// addColumnIfMissing adds a column only if it is not present yet, so older
// databases can be upgraded in place.
func (s *Store) addColumnIfMissing(table, col, def string) error {
	var name string
	err := s.db.QueryRow("SELECT name FROM pragma_table_info(?) WHERE name = ?", table, col).Scan(&name)
	if err == sql.ErrNoRows {
		_, err = s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col, def))
		return err
	}
	return err // nil when the column already exists
}

// CreateVideo inserts a new video record and returns its ID.
func (s *Store) CreateVideo(v *models.Video) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO videos (title, filename, filepath, mime_type, size, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		v.Title, v.Filename, v.FilePath, v.MimeType, v.Size, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListVideos returns a page of videos ordered newest first, with each video's
// like state for the given user (userID=0 means anonymous → liked is always false).
// afterID=0 means first page.
func (s *Store) ListVideos(userID int64, afterID int64, limit int) ([]models.VideoWithLike, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT v.id, v.title, v.filename, v.filepath, v.mime_type, v.size,
		       v.likes_count, v.comments_count, v.views, v.created_at,
		       EXISTS(SELECT 1 FROM likes l WHERE l.user_id = ? AND l.video_id = v.id) AS liked
		FROM videos v
		WHERE (? = 0 OR v.id < ?)
		ORDER BY v.id DESC
		LIMIT ?`,
		userID, afterID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.VideoWithLike
	for rows.Next() {
		var v models.Video
		var created int64
		var liked bool
		if err := rows.Scan(&v.ID, &v.Title, &v.Filename, &v.FilePath, &v.MimeType,
			&v.Size, &v.LikesCount, &v.CommentsCount, &v.Views, &created, &liked); err != nil {
			return nil, err
		}
		v.CreatedAt = time.Unix(created, 0)
		out = append(out, models.VideoWithLike{Video: v, Liked: liked})
	}
	return out, rows.Err()
}

// GetVideo returns a single video with the requesting user's like state.
func (s *Store) GetVideo(userID int64, id int64) (*models.VideoWithLike, error) {
	row := s.db.QueryRow(`
		SELECT v.id, v.title, v.filename, v.filepath, v.mime_type, v.size,
		       v.likes_count, v.comments_count, v.views, v.created_at,
		       EXISTS(SELECT 1 FROM likes l WHERE l.user_id = ? AND l.video_id = v.id) AS liked
		FROM videos v
		WHERE v.id = ?`, userID, id)
	var v models.Video
	var created int64
	var liked bool
	err := row.Scan(&v.ID, &v.Title, &v.Filename, &v.FilePath, &v.MimeType,
		&v.Size, &v.LikesCount, &v.CommentsCount, &v.Views, &created, &liked)
	if err != nil {
		return nil, err
	}
	v.CreatedAt = time.Unix(created, 0)
	return &models.VideoWithLike{Video: v, Liked: liked}, nil
}

// ToggleLike toggles a like for the given user/video and returns the new
// state together with the recomputed like count.
func (s *Store) ToggleLike(userID int64, videoID int64) (liked bool, count int64, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO likes (user_id, video_id, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, video_id) DO NOTHING`, userID, videoID, time.Now().Unix())
	if err != nil {
		return false, 0, err
	}
	affected, _ := res.RowsAffected()
	liked = affected > 0

	if !liked {
		// Already liked -> remove it.
		if _, err = tx.Exec(`DELETE FROM likes WHERE user_id = ? AND video_id = ?`, userID, videoID); err != nil {
			return false, 0, err
		}
	}

	if err = tx.QueryRow(`SELECT COUNT(*) FROM likes WHERE video_id = ?`, videoID).Scan(&count); err != nil {
		return false, 0, err
	}
	if _, err = tx.Exec(`UPDATE videos SET likes_count = ? WHERE id = ?`, count, videoID); err != nil {
		return false, 0, err
	}

	if err = tx.Commit(); err != nil {
		return false, 0, err
	}
	return liked, count, nil
}

// CreateComment inserts a comment and returns it together with the new comment
// count for the video. The denormalized comments_count on videos is refreshed.
// author is the display name of the commenting user.
func (s *Store) CreateComment(userID int64, author string, videoID int64, text string) (*models.Comment, int64, error) {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`INSERT INTO comments (user_id, video_id, text, created_at) VALUES (?, ?, ?, ?)`,
		userID, videoID, text, now,
	)
	if err != nil {
		return nil, 0, err
	}
	id, _ := res.LastInsertId()

	var count int64
	if err = tx.QueryRow(`SELECT COUNT(*) FROM comments WHERE video_id = ?`, videoID).Scan(&count); err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec(`UPDATE videos SET comments_count = ? WHERE id = ?`, count, videoID); err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}

	return &models.Comment{
		ID:        id,
		VideoID:   videoID,
		Author:    author,
		Text:      text,
		CreatedAt: time.Unix(now, 0),
	}, count, nil
}

// ListComments returns a page of comments for a video, newest first. The author
// name is resolved via a LEFT JOIN on the users table. afterID=0 means first page.
func (s *Store) ListComments(videoID int64, afterID int64, limit int) ([]models.Comment, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT c.id, c.video_id, COALESCE(u.name, 'deleted user'), c.text, c.created_at
		FROM comments c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE c.video_id = ? AND (? = 0 OR c.id < ?)
		ORDER BY c.id DESC
		LIMIT ?`,
		videoID, afterID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Comment
	for rows.Next() {
		var c models.Comment
		var created int64
		if err := rows.Scan(&c.ID, &c.VideoID, &c.Author, &c.Text, &created); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(created, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

// IncrementViews bumps the view counter for a video.
func (s *Store) IncrementViews(id int64) {
	s.db.Exec(`UPDATE videos SET views = views + 1 WHERE id = ?`, id)
}

// CreateOrUpdateUser inserts a user for the given provider identity, or updates
// the name/email/avatar if the identity already exists. Returns the user.
func (s *Store) CreateOrUpdateUser(provider, providerUserID, name, email, avatarURL string) (*models.User, error) {
	now := time.Now().Unix()
	_, err := s.db.Exec(`INSERT INTO users (provider, provider_user_id, name, email, avatar_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, provider_user_id) DO UPDATE SET
			name = excluded.name,
			email = excluded.email,
			avatar_url = excluded.avatar_url`,
		provider, providerUserID, name, email, avatarURL, now)
	if err != nil {
		return nil, err
	}
	return s.getUser(`SELECT id, provider, provider_user_id, name, email, avatar_url, created_at
		FROM users WHERE provider = ? AND provider_user_id = ?`, provider, providerUserID)
}

// GetUser returns a user by id.
func (s *Store) GetUser(id int64) (*models.User, error) {
	return s.getUser(`SELECT id, provider, provider_user_id, name, email, avatar_url, created_at
		FROM users WHERE id = ?`, id)
}

// GetUserBySession returns the user behind a live (non-expired) session token.
// Returns sql.ErrNoRows when the token is unknown or expired.
func (s *Store) GetUserBySession(token string) (*models.User, error) {
	return s.getUser(`SELECT u.id, u.provider, u.provider_user_id, u.name, u.email, u.avatar_url, u.created_at
		FROM users u
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token = ? AND s.expires_at > ?`, token, time.Now().Unix())
}

func (s *Store) getUser(query string, args ...any) (*models.User, error) {
	var u models.User
	var created int64
	err := s.db.QueryRow(query, args...).Scan(
		&u.ID, &u.Provider, &u.ProviderUserID, &u.Name, &u.Email, &u.AvatarURL, &created)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = time.Unix(created, 0)
	return &u, nil
}

// CreateSession starts a new session for a user with the given token and ttl.
func (s *Store) CreateSession(userID int64, token string, ttl time.Duration) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`INSERT INTO sessions (token, user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)`, token, userID, now, now+int64(ttl.Seconds()))
	return err
}

// DeleteSession removes a session token (logout).
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }
