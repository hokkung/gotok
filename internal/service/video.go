// Package service contains the business-logic layer for GoTok, sitting between
// the HTTP handlers (transport) and the store (persistence). Each service owns
// a bounded context and defines the store interface it consumes.
package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/hokkung/gotok/internal/models"
)

// maxCommentLen is the hard cap on a single comment's length.
const maxCommentLen = 500

// ErrVideoNotFound is returned when a referenced video does not exist.
var ErrVideoNotFound = errors.New("video not found")

// ErrEmptyComment is returned when a comment has no visible text.
var ErrEmptyComment = errors.New("comment cannot be empty")

// VideoStore is the persistence interface consumed by VideoService.
type VideoStore interface {
	ListVideos(ctx context.Context, viewerID, afterID int64, limit int) ([]models.VideoWithLike, error)
	ListVideosByUser(ctx context.Context, ownerID, viewerID, afterID int64, limit int) ([]models.VideoWithLike, error)
	ListLikedVideos(ctx context.Context, ownerID, viewerID, afterLikeID int64, limit int) ([]models.VideoWithLike, int64, error)
	GetVideo(ctx context.Context, userID, id int64) (*models.VideoWithLike, error)
	ToggleLike(ctx context.Context, userID, videoID int64) (bool, int64, error)
	IncrementViews(ctx context.Context, id int64)
	CreateComment(ctx context.Context, userID int64, author string, videoID int64, text string) (*models.Comment, int64, error)
	ListComments(ctx context.Context, videoID, afterID int64, limit int) ([]models.Comment, error)
	CreateVideo(ctx context.Context, v *models.Video) (int64, error)
}

// VideoService contains the business logic for the feed, likes, comments, and
// views.
type VideoService struct {
	store VideoStore
}

// NewVideoService creates a VideoService backed by the given store.
func NewVideoService(s VideoStore) *VideoService {
	return &VideoService{store: s}
}

// ListFeedVideos returns one page of the global feed plus the next pagination
// cursor (0 when the page is exhausted).
func (s *VideoService) ListFeedVideos(ctx context.Context, viewerID, cursor int64, limit int) ([]models.VideoWithLike, int64, error) {
	videos, err := s.store.ListVideos(ctx, viewerID, cursor, limit)
	if err != nil {
		return nil, 0, err
	}
	return videos, nextVideoCursor(videos), nil
}

// ListUserVideos returns one page of a user's uploaded videos.
func (s *VideoService) ListUserVideos(ctx context.Context, ownerID, viewerID, cursor int64, limit int) ([]models.VideoWithLike, int64, error) {
	videos, err := s.store.ListVideosByUser(ctx, ownerID, viewerID, cursor, limit)
	if err != nil {
		return nil, 0, err
	}
	return videos, nextVideoCursor(videos), nil
}

// ListUserLikedVideos returns one page of videos a user has liked.
func (s *VideoService) ListUserLikedVideos(ctx context.Context, ownerID, viewerID, cursor int64, limit int) ([]models.VideoWithLike, int64, error) {
	videos, next, err := s.store.ListLikedVideos(ctx, ownerID, viewerID, cursor, limit)
	if err != nil {
		return nil, 0, err
	}
	return videos, next, nil
}

// ToggleLike flips the requesting user's like on a video and returns the new
// state together with the recomputed like count. Returns ErrVideoNotFound when
// the video does not exist.
func (s *VideoService) ToggleLike(ctx context.Context, userID, videoID int64) (bool, int64, error) {
	if _, err := s.store.GetVideo(ctx, userID, videoID); err != nil {
		return false, 0, ErrVideoNotFound
	}
	return s.store.ToggleLike(ctx, userID, videoID)
}

// IncrementViews bumps the view counter for a video (fire-and-forget).
func (s *VideoService) IncrementViews(ctx context.Context, videoID int64) {
	s.store.IncrementViews(ctx, videoID)
}

// CreateComment validates the comment text, ensures the video exists, and
// inserts the comment. The author name is resolved from the user identity.
func (s *VideoService) CreateComment(ctx context.Context, userID int64, author string, videoID int64, text string) (*models.Comment, int64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, 0, ErrEmptyComment
	}
	if utf8.RuneCountInString(text) > maxCommentLen {
		text = string([]rune(text)[:maxCommentLen])
	}
	if _, err := s.store.GetVideo(ctx, userID, videoID); err != nil {
		return nil, 0, ErrVideoNotFound
	}
	return s.store.CreateComment(ctx, userID, author, videoID, text)
}

// ListComments returns one page of comments for a video plus the next cursor.
func (s *VideoService) ListComments(ctx context.Context, videoID, cursor int64, limit int) ([]models.Comment, int64, error) {
	comments, err := s.store.ListComments(ctx, videoID, cursor, limit)
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(comments) > 0 {
		next = comments[len(comments)-1].ID
	}
	return comments, next, nil
}

// CreateVideo records a newly uploaded video's metadata and returns its ID.
func (s *VideoService) CreateVideo(ctx context.Context, v *models.Video) (int64, error) {
	return s.store.CreateVideo(ctx, v)
}

// nextVideoCursor returns the ID of the last video for keyset pagination, or 0
// when the slice is empty.
func nextVideoCursor(videos []models.VideoWithLike) int64 {
	if len(videos) > 0 {
		return videos[len(videos)-1].ID
	}
	return 0
}
