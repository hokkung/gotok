// Package store is the PostgreSQL persistence layer for GoTok: it owns schema
// migrations (via golang-migrate) and all SQL queries.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the pgx driver for database/sql

	"go.uber.org/zap"

	"github.com/hokkung/gotok/internal/models"
)

// ErrEmailExists is returned when creating an email/password account whose email
// is already registered.
var ErrEmailExists = errors.New("email already registered")

// Store wraps the database connection and provides data access.
type Store struct {
	db     *sql.DB
	logger *zap.Logger
}

// New opens the PostgreSQL database, runs migrations, and returns a Store. A
// nil logger is replaced with a no-op logger.
func New(ctx context.Context, databaseURL string, lg *zap.Logger) (*Store, error) {
	if lg == nil {
		lg = zap.NewNop()
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	s := &Store{db: db, logger: lg}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// migrate applies all pending schema migrations from the embedded SQL files.
func (s *Store) migrate() error {
	source, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}
	driver, err := postgres.WithInstance(s.db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// isUniqueConstraintErr reports whether err is a PostgreSQL unique-violation
// (error code 23505).
func isUniqueConstraintErr(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation
	}
	return false
}

// CreateVideo inserts a new video record and returns its ID.
func (s *Store) CreateVideo(ctx context.Context, v *models.Video) (int64, error) {
	now := time.Now().Unix()
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO videos (user_id, title, filename, filepath, mime_type, size, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		v.UserID, v.Title, v.Filename, v.FilePath, v.MimeType, v.Size, now,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListVideos returns a page of videos ordered newest first, with each video's
// like state for the given viewer (viewerID=0 means anonymous → liked is always
// false). afterID=0 means first page.
func (s *Store) ListVideos(ctx context.Context, viewerID int64, afterID int64, limit int) ([]models.VideoWithLike, error) {
	return s.listVideosPage(ctx, viewerID, 0, afterID, limit)
}

// ListVideosByUser returns a page of a single owner's videos, newest first, with
// the requesting viewer's like state. ownerID is the uploader whose videos are
// listed; viewerID=0 means anonymous.
func (s *Store) ListVideosByUser(ctx context.Context, ownerID, viewerID, afterID int64, limit int) ([]models.VideoWithLike, error) {
	return s.listVideosPage(ctx, viewerID, ownerID, afterID, limit)
}

// listVideosPage is the shared keyset query behind ListVideos and
// ListVideosByUser. ownerID <= 0 means no owner filter (the global feed).
func (s *Store) listVideosPage(ctx context.Context, viewerID, ownerID, afterID int64, limit int) ([]models.VideoWithLike, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.id, v.user_id, v.title, v.filename, v.filepath, v.mime_type, v.size,
		       COALESCE(u.name, '') AS author_name,
		       v.likes_count, v.comments_count, v.views, v.created_at,
		       EXISTS(SELECT 1 FROM likes l WHERE l.user_id = $1 AND l.video_id = v.id) AS liked
		FROM videos v
		LEFT JOIN users u ON u.id = v.user_id
		WHERE ($2 = 0 OR v.id < $2)
		  AND ($3 = 0 OR v.user_id = $3)
		ORDER BY v.id DESC
		LIMIT $4`,
		viewerID, afterID, ownerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.VideoWithLike
	for rows.Next() {
		var v models.Video
		var created int64
		var liked bool
		if err := rows.Scan(&v.ID, &v.UserID, &v.Title, &v.Filename, &v.FilePath, &v.MimeType,
			&v.Size, &v.AuthorName, &v.LikesCount, &v.CommentsCount, &v.Views, &created, &liked); err != nil {
			return nil, err
		}
		v.CreatedAt = time.Unix(created, 0)
		out = append(out, models.VideoWithLike{Video: v, Liked: liked})
	}
	return out, rows.Err()
}

// CountVideosByUser returns how many videos a user has uploaded.
func (s *Store) CountVideosByUser(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// ListLikedVideos returns a page of videos the given owner has liked, most
// recently liked first, with the requesting viewer's like state. Pagination is
// keyed on the like row's id (afterLikeID=0 means first page). It returns the
// videos together with the next cursor (the last returned like id, or 0 when
// the page is exhausted).
func (s *Store) ListLikedVideos(ctx context.Context, ownerID, viewerID, afterLikeID int64, limit int) ([]models.VideoWithLike, int64, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.id, v.user_id, v.title, v.filename, v.filepath, v.mime_type, v.size,
		       COALESCE(u.name, '') AS author_name,
		       v.likes_count, v.comments_count, v.views, v.created_at,
		       EXISTS(SELECT 1 FROM likes l2 WHERE l2.user_id = $1 AND l2.video_id = v.id) AS liked,
		       l.id AS like_id
		FROM likes l
		JOIN videos v ON v.id = l.video_id
		LEFT JOIN users u ON u.id = v.user_id
		WHERE l.user_id = $2 AND ($3 = 0 OR l.id < $3)
		ORDER BY l.id DESC
		LIMIT $4`,
		viewerID, ownerID, afterLikeID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []models.VideoWithLike
	var lastLikeID int64
	for rows.Next() {
		var v models.Video
		var created int64
		var liked bool
		var likeID int64
		if err := rows.Scan(&v.ID, &v.UserID, &v.Title, &v.Filename, &v.FilePath, &v.MimeType,
			&v.Size, &v.AuthorName, &v.LikesCount, &v.CommentsCount, &v.Views, &created, &liked, &likeID); err != nil {
			return nil, 0, err
		}
		v.CreatedAt = time.Unix(created, 0)
		out = append(out, models.VideoWithLike{Video: v, Liked: liked})
		lastLikeID = likeID
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) < limit {
		return out, 0, nil
	}
	return out, lastLikeID, nil
}

// CountLikedVideos returns how many videos a user has liked.
func (s *Store) CountLikedVideos(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM likes WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// GetVideo returns a single video with the requesting user's like state.
func (s *Store) GetVideo(ctx context.Context, userID int64, id int64) (*models.VideoWithLike, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT v.id, v.user_id, v.title, v.filename, v.filepath, v.mime_type, v.size,
		       COALESCE(u.name, '') AS author_name,
		       v.likes_count, v.comments_count, v.views, v.created_at,
		       EXISTS(SELECT 1 FROM likes l WHERE l.user_id = $1 AND l.video_id = v.id) AS liked
		FROM videos v
		LEFT JOIN users u ON u.id = v.user_id
		WHERE v.id = $2`, userID, id)
	var v models.Video
	var created int64
	var liked bool
	err := row.Scan(&v.ID, &v.UserID, &v.Title, &v.Filename, &v.FilePath, &v.MimeType,
		&v.Size, &v.AuthorName, &v.LikesCount, &v.CommentsCount, &v.Views, &created, &liked)
	if err != nil {
		return nil, err
	}
	v.CreatedAt = time.Unix(created, 0)
	return &models.VideoWithLike{Video: v, Liked: liked}, nil
}

// ToggleLike toggles a like for the given user/video and returns the new
// state together with the recomputed like count.
func (s *Store) ToggleLike(ctx context.Context, userID int64, videoID int64) (liked bool, count int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `INSERT INTO likes (user_id, video_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT(user_id, video_id) DO NOTHING`, userID, videoID, time.Now().Unix())
	if err != nil {
		return false, 0, err
	}
	affected, _ := res.RowsAffected()
	liked = affected > 0

	if !liked {
		if _, err = tx.ExecContext(ctx, `DELETE FROM likes WHERE user_id = $1 AND video_id = $2`, userID, videoID); err != nil {
			return false, 0, err
		}
	}

	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM likes WHERE video_id = $1`, videoID).Scan(&count); err != nil {
		return false, 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE videos SET likes_count = $1 WHERE id = $2`, count, videoID); err != nil {
		return false, 0, err
	}

	if err = tx.Commit(); err != nil {
		return false, 0, err
	}
	return liked, count, nil
}

// CreateComment inserts a comment and returns it together with the new comment
// count for the video. The denormalized comments_count on videos is refreshed.
func (s *Store) CreateComment(ctx context.Context, userID int64, author string, videoID int64, text string) (*models.Comment, int64, error) {
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO comments (user_id, video_id, text, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, videoID, text, now,
	).Scan(&id)
	if err != nil {
		return nil, 0, err
	}

	var count int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE video_id = $1`, videoID).Scan(&count); err != nil {
		return nil, 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE videos SET comments_count = $1 WHERE id = $2`, count, videoID); err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}

	return &models.Comment{
		ID:        id,
		VideoID:   videoID,
		UserID:    userID,
		Author:    author,
		Text:      text,
		CreatedAt: time.Unix(now, 0),
	}, count, nil
}

// ListComments returns a page of comments for a video, newest first. The author
// name and avatar are resolved via a LEFT JOIN on the users table. afterID=0
// means first page.
func (s *Store) ListComments(ctx context.Context, videoID int64, afterID int64, limit int) ([]models.Comment, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.video_id, c.user_id, COALESCE(u.name, 'deleted user'), COALESCE(u.avatar_url, ''), c.text, c.created_at
		FROM comments c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE c.video_id = $1 AND ($2 = 0 OR c.id < $2)
		ORDER BY c.id DESC
		LIMIT $3`,
		videoID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Comment
	for rows.Next() {
		var c models.Comment
		var created int64
		if err := rows.Scan(&c.ID, &c.VideoID, &c.UserID, &c.Author, &c.AvatarURL, &c.Text, &created); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(created, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

// IncrementViews bumps the view counter for a video.
func (s *Store) IncrementViews(ctx context.Context, id int64) {
	if _, err := s.db.ExecContext(ctx, `UPDATE videos SET views = views + 1 WHERE id = $1`, id); err != nil {
		s.logger.Error("increment views", zap.Int64("video_id", id), zap.Error(err))
	}
}

// CreateOrUpdateUser inserts a user for the given provider identity, or updates
// the name/email/avatar if the identity already exists. Returns the user.
func (s *Store) CreateOrUpdateUser(ctx context.Context, provider, providerUserID, name, email, avatarURL string) (*models.User, error) {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (provider, provider_user_id, name, email, avatar_url, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT(provider, provider_user_id) DO UPDATE SET
			name = EXCLUDED.name,
			email = EXCLUDED.email,
			avatar_url = EXCLUDED.avatar_url`,
		provider, providerUserID, name, email, avatarURL, now)
	if err != nil {
		return nil, err
	}
	return s.getUser(ctx, `SELECT id, provider, provider_user_id, name, email, avatar_url, password_hash, bio, created_at
		FROM users WHERE provider = $1 AND provider_user_id = $2`, provider, providerUserID)
}

// GetUserByEmail returns the email/password account for the given email. The
// account's provider_user_id is the email itself, so the UNIQUE(provider,
// provider_user_id) constraint guarantees at most one match. Returns
// sql.ErrNoRows when no such account exists.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return s.getUser(ctx, `SELECT id, provider, provider_user_id, name, email, avatar_url, password_hash, bio, created_at
		FROM users WHERE provider = 'email' AND provider_user_id = $1`, email)
}

// CreateUserWithPassword inserts a new email/password account. provider_user_id
// is set to the (normalized) email so the UNIQUE constraint enforces email
// uniqueness; a duplicate returns ErrEmailExists.
func (s *Store) CreateUserWithPassword(ctx context.Context, name, email, passwordHash string) (*models.User, error) {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (provider, provider_user_id, name, email, password_hash, created_at)
		VALUES ('email', $1, $2, $3, $4, $5)`,
		email, name, email, passwordHash, now)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return nil, ErrEmailExists
		}
		return nil, err
	}
	return s.GetUserByEmail(ctx, email)
}

// GetUser returns a user by id.
func (s *Store) GetUser(ctx context.Context, id int64) (*models.User, error) {
	return s.getUser(ctx, `SELECT id, provider, provider_user_id, name, email, avatar_url, password_hash, bio, created_at
		FROM users WHERE id = $1`, id)
}

// UpdateProfile updates the editable profile fields (name, bio, avatar) for a
// user. avatarURL is the web path of a freshly uploaded avatar (e.g.
// "/uploads/avatar-xxxx.jpg"); pass empty to keep the existing avatar.
func (s *Store) UpdateProfile(ctx context.Context, userID int64, name, bio, avatarURL string) (*models.User, error) {
	if avatarURL != "" {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET name = $1, bio = $2, avatar_url = $3 WHERE id = $4`,
			name, bio, avatarURL, userID); err != nil {
			return nil, err
		}
	} else {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET name = $1, bio = $2 WHERE id = $3`,
			name, bio, userID); err != nil {
			return nil, err
		}
	}
	return s.GetUser(ctx, userID)
}

// GetUserBySession returns the user behind a live (non-expired) session token.
// Returns sql.ErrNoRows when the token is unknown or expired.
func (s *Store) GetUserBySession(ctx context.Context, token string) (*models.User, error) {
	return s.getUser(ctx, `SELECT u.id, u.provider, u.provider_user_id, u.name, u.email, u.avatar_url, u.password_hash, u.bio, u.created_at
		FROM users u
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > $2`, token, time.Now().Unix())
}

func (s *Store) getUser(ctx context.Context, query string, args ...any) (*models.User, error) {
	var u models.User
	var created int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&u.ID, &u.Provider, &u.ProviderUserID, &u.Name, &u.Email, &u.AvatarURL, &u.PasswordHash, &u.Bio, &created)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = time.Unix(created, 0)
	return &u, nil
}

// CreateSession starts a new session for a user with the given token and ttl.
func (s *Store) CreateSession(ctx context.Context, userID int64, token string, ttl time.Duration) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token, user_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4)`, token, userID, now, now+int64(ttl.Seconds()))
	return err
}

// DeleteSession removes a session token (logout).
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }
