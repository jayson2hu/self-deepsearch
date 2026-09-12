package catalog

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("catalog entity not found")

type WorkSummary struct {
	ID          string
	Code        string
	Title       string
	ReleaseDate *time.Time
	StudioName  string
	Slug        string
	ImageURL    *string
	Image       *Image
}

func (item WorkSummary) CursorID() string { return item.ID }

type PerformerSummary struct {
	ID       string
	Name     string
	Slug     string
	ImageURL *string
	Image    *Image
}

func (item PerformerSummary) CursorID() string { return item.ID }

type StudioSummary struct {
	ID   string
	Name string
	Slug string
}

func (item StudioSummary) CursorID() string { return item.ID }

type StudioDetail struct {
	StudioSummary
	Works []WorkSummary
}

type PerformerDetail struct {
	PerformerSummary
	NameOriginal   *string
	RomanizedName  *string
	Aliases        []string
	ActivityStatus string
	Agency         *string
	BirthYear      *int
	HeightCM       *int
	Measurements   *string
	DebutYear      *int
	Works          []WorkSummary
	Images         []Image
}

type WorkDetail struct {
	WorkSummary
	TitleOriginal *string
	Summary       *string
	Performers    []PerformerSummary
	Images        []Image
	RelatedWorks  []WorkSummary
}

type Image struct {
	URL        string
	Rendition  string
	Width      int
	Height     int
	MimeType   string
	Renditions []ImageRendition
}

type ImageRendition struct {
	URL       string `json:"url"`
	Rendition string `json:"rendition"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MimeType  string `json:"mime_type"`
}

type SitemapEntry struct {
	EntityType string
	Slug       string
	UpdatedAt  time.Time
}

type DiscoveryMixRule struct {
	WorkSlots      int
	PerformerSlots int
	RepeatWindow   int
}

type RedirectTarget struct {
	TargetPath string
	StatusCode int
}

type Repository interface {
	LatestWorks(context.Context, string, int) ([]WorkSummary, error)
	RecentlyAddedWorks(context.Context, string, int) ([]WorkSummary, error)
	EditorialWorks(context.Context, int) ([]WorkSummary, error)
	LatestPerformers(context.Context, int) ([]PerformerSummary, error)
	DiscoveryMixRule(context.Context) (DiscoveryMixRule, error)
	TrendingWorks(context.Context, time.Duration, string, int) ([]WorkSummary, error)
	MostViewedWorks(context.Context, *time.Duration, string, int) ([]WorkSummary, error)
	SearchWorks(context.Context, string, string, string, int) ([]WorkSummary, error)
	WorkBySlug(context.Context, string) (WorkDetail, error)
	ListPerformers(context.Context, string, int) ([]PerformerSummary, error)
	PerformerBySlug(context.Context, string) (PerformerDetail, error)
	ListStudios(context.Context, string, int) ([]StudioSummary, error)
	StudioBySlug(context.Context, string) (StudioDetail, error)
	RecordPageView(context.Context, string, string) error
	RecordSearchOutcome(context.Context, bool) error
	SitemapEntries(context.Context, string, int) ([]SitemapEntry, error)
	RedirectTarget(context.Context, string) (RedirectTarget, error)
}
