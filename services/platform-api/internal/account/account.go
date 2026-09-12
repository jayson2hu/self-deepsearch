package account

import (
	"context"
	"errors"
	"time"

	"self-deepsearch/services/platform-api/internal/catalog"
)

var ErrNotFound = errors.New("account content not found")
var ErrConflict = errors.New("account state conflict")

type HistoryItem struct {
	// ContentType identifies the public entity represented by this history
	// entry. The API intentionally uses the same compact shape as HomeItem so
	// work, performer and studio detail visits can be rendered uniformly.
	ContentType string
	ID          string
	Code        string
	Title       string
	Subtitle    string
	Href        string
	ImageURL    *string
	ViewedAt    time.Time
}

type Feedback struct {
	ID           string
	ContentType  *string
	ContentID    *string
	FeedbackType string
	Message      string
	EvidenceURL  *string
	ReviewStatus string
	Submitter    string
	Reviewer     *string
	ReviewedAt   *time.Time
	CreatedAt    time.Time
}

func ValidFeedbackReviewTransition(current, target string) bool {
	if current == "pending" {
		return target == "reviewing" || target == "accepted" || target == "rejected" || target == "closed"
	}
	if current == "reviewing" {
		return target == "accepted" || target == "rejected" || target == "closed"
	}
	return false
}

type Repository interface {
	SetFavorite(context.Context, string, string, bool, time.Time) error
	ListFavorites(context.Context, string, int) ([]catalog.WorkSummary, error)
	SetFollow(context.Context, string, string, bool, time.Time) error
	ListFollows(context.Context, string, int) ([]catalog.PerformerSummary, error)
	ListFollowedWorks(context.Context, string, int) ([]catalog.WorkSummary, error)
	SetHiddenWork(context.Context, string, string, bool, time.Time) error
	ListHiddenWorks(context.Context, string, int) ([]catalog.WorkSummary, error)
	RecordHistory(context.Context, string, string, string, time.Time) error
	ListHistory(context.Context, string, int) ([]HistoryItem, error)
	ClearHistory(context.Context, string, time.Time) (int64, error)
	CreateFeedback(context.Context, string, *string, *string, string, string, *string, time.Time) (Feedback, error)
	ListFeedback(context.Context, string, int) ([]Feedback, error)
	ListFeedbackForReview(context.Context, string, int) ([]Feedback, error)
	ReviewFeedback(context.Context, string, string, string, string, string, time.Time) (Feedback, error)
}
