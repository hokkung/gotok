// Package models defines the data types (Video, User, Comment, Like) shared
// across GoTok's store and handler layers.
package models

import "time"

// Video represents an uploaded video record.
type Video struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"` // uploader; 0 for legacy videos
	Title         string    `json:"title"`
	Filename      string    `json:"filename"`
	FilePath      string    `json:"-"` // server-only path on disk
	MimeType      string    `json:"mime_type"`
	Size          int64     `json:"size"`
	AuthorName    string    `json:"author_name"` // uploader display name ("" when unknown)
	LikesCount    int64     `json:"likes_count"`
	CommentsCount int64     `json:"comments_count"`
	Views         int64     `json:"views"`
	CreatedAt     time.Time `json:"created_at"`
}

// VideoWithLike bundles a video with the requesting client's like state.
type VideoWithLike struct {
	Video
	Liked bool `json:"liked"`
}

// Like represents a single user's like on a video.
type Like struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	VideoID   int64     `json:"video_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Comment represents a user comment on a video. Author is the commenting
// user's display name (looked up via the users table).
type Comment struct {
	ID        int64     `json:"id"`
	VideoID   int64     `json:"video_id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// User represents a logged-in identity. The "email" provider covers
// email/password accounts; "google" and "facebook" will be populated once SSO
// is implemented.
type User struct {
	ID             int64     `json:"id"`
	Provider       string    `json:"provider"` // "email" | "demo" | "google" | "facebook"
	ProviderUserID string    `json:"-"`        // provider-specific id; never sent to clients
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	AvatarURL      string    `json:"avatar_url"`
	PasswordHash   string    `json:"-"` // bcrypt hash for "email" accounts; "" otherwise
	Bio            string    `json:"bio"`
	CreatedAt      time.Time `json:"created_at"`
}
