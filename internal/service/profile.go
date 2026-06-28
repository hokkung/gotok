package service

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/hokkung/gotok/internal/models"
)

// MaxBioRunes caps the profile bio length. Exported so handlers can include it
// in validation error messages.
const MaxBioRunes = 160

// ErrUserNotFound is returned when a requested user does not exist.
var ErrUserNotFound = errors.New("user not found")

// ErrBioTooLong is returned when the bio exceeds the maximum length.
var ErrBioTooLong = errors.New("bio too long")

// ErrNameRequired is returned when the display name is empty during a profile
// edit.
var ErrProfileNameRequired = errors.New("name is required")

// ProfileStore is the persistence interface consumed by ProfileService.
type ProfileStore interface {
	GetUser(ctx context.Context, id int64) (*models.User, error)
	UpdateProfile(ctx context.Context, userID int64, name, bio, avatarURL string) (*models.User, error)
	CountVideosByUser(ctx context.Context, userID int64) (int64, error)
	CountLikedVideos(ctx context.Context, userID int64) (int64, error)
}

// ProfileResult bundles the data needed to render a profile page.
type ProfileResult struct {
	User       *models.User
	VideoCount int64
	LikedCount int64
}

// ProfileService handles profile viewing and editing.
type ProfileService struct {
	store ProfileStore
}

// NewProfileService creates a ProfileService backed by the given store.
func NewProfileService(s ProfileStore) *ProfileService {
	return &ProfileService{store: s}
}

// GetProfile returns the user together with their video and liked counts.
// Returns ErrUserNotFound when the user does not exist.
func (s *ProfileService) GetProfile(ctx context.Context, userID int64) (*ProfileResult, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	videoCount, _ := s.store.CountVideosByUser(ctx, userID)
	likedCount, _ := s.store.CountLikedVideos(ctx, userID)
	return &ProfileResult{
		User:       user,
		VideoCount: videoCount,
		LikedCount: likedCount,
	}, nil
}

// UpdateProfile validates the editable fields and persists them. avatarURL is
// empty when the caller wants to keep the existing avatar.
func (s *ProfileService) UpdateProfile(ctx context.Context, userID int64, name, bio, avatarURL string) (*models.User, error) {
	if name == "" {
		return nil, ErrProfileNameRequired
	}
	if utf8.RuneCountInString(bio) > MaxBioRunes {
		return nil, ErrBioTooLong
	}
	return s.store.UpdateProfile(ctx, userID, name, bio, avatarURL)
}
