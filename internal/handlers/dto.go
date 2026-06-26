package handlers

import "github.com/hokkung/gotok/internal/models"

// The structs below are request/response contracts used only for Swagger
// (OpenAPI) schema generation. Handlers still build their JSON with gin.H so
// there is no runtime behaviour change; swag resolves these types from the
// @Success/@Param annotations, not from the actual returned values.

// ErrorResponse is the standard error envelope: {"error": "..."}.
type ErrorResponse struct {
	Error string `json:"error" example:"something went wrong"`
}

// OkResponse is the minimal success envelope: {"ok": true}.
type OkResponse struct {
	Ok bool `json:"ok" example:"true"`
}

// MeResponse wraps the logged-in user (nil when anonymous).
type MeResponse struct {
	User *models.User `json:"user"`
}

// ListVideosResponse is a cursor-paginated page of feed/profile videos.
type ListVideosResponse struct {
	Videos []models.VideoWithLike `json:"videos"`
	Next   int64                  `json:"next" example:"42"`
}

// ToggleLikeResponse is the new like state after toggling.
type ToggleLikeResponse struct {
	Liked bool  `json:"liked" example:"true"`
	Count int64 `json:"count" example:"7"`
}

// ListCommentsResponse is a cursor-paginated page of comments.
type ListCommentsResponse struct {
	Comments []models.Comment `json:"comments"`
	Next     int64            `json:"next" example:"42"`
}

// CreateCommentResponse is the created comment plus the refreshed comment count.
type CreateCommentResponse struct {
	Comment models.Comment `json:"comment"`
	Count   int64          `json:"count" example:"3"`
}

// UploadResponse is the metadata returned after a successful video upload.
type UploadResponse struct {
	ID       int64  `json:"id" example:"12"`
	Filename string `json:"filename" example:"1719400000000-1a2b3c.mp4"`
	Title    string `json:"title" example:"My first clip"`
}

// EditProfileResponse wraps the updated user after a profile edit.
type EditProfileResponse struct {
	User *models.User `json:"user"`
}

// LoginDemoResponse is the body returned by the demo login endpoint.
type LoginDemoResponse struct {
	Ok       bool         `json:"ok" example:"true"`
	User     *models.User `json:"user"`
	Redirect string       `json:"redirect" example:"/feed"`
}
