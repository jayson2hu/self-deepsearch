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

type RelatedWorkCursor struct {
	ReleaseDate *time.Time
	PublishedAt time.Time
	ID          string
}

type RelatedWorkResult struct {
	WorkSummary
	Cursor RelatedWorkCursor
}

func (item RelatedWorkResult) RelatedCursor() RelatedWorkCursor { return item.Cursor }

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

type SearchCursor struct {
	Rank int
	Name string
	ID   string
}

type PerformerSearchResult struct {
	PerformerSummary
	Cursor SearchCursor
}

func (item PerformerSearchResult) RankedCursor() SearchCursor { return item.Cursor }

type StudioSearchResult struct {
	StudioSummary
	Cursor SearchCursor
}

func (item StudioSearchResult) RankedCursor() SearchCursor { return item.Cursor }

type StudioDetail struct {
	StudioSummary
	PublicUpdatedAt *time.Time
	ViewCount30D    int64
	ViewCountTotal  int64
	Works           []WorkSummary
	SEO             SEOProfile
}

type PerformerDetail struct {
	PerformerSummary
	PublicUpdatedAt *time.Time
	ViewCount30D    int64
	ViewCountTotal  int64
	NameOriginal    *string
	RomanizedName   *string
	Aliases         []string
	ActivityStatus  string
	Agency          *string
	BirthYear       *int
	HeightCM        *int
	Measurements    *string
	DebutYear       *int
	Works           []WorkSummary
	Images          []Image
	SEO             SEOProfile
}

type WorkDetail struct {
	WorkSummary
	PublicUpdatedAt *time.Time
	ViewCount30D    int64
	ViewCountTotal  int64
	Studio          *StudioSummary
	TitleOriginal   *string
	Summary         *string
	Performers      []PerformerSummary
	Images          []Image
	RelatedWorks    []WorkSummary
	SEO             SEOProfile
}

type SEOProfile struct {
	Indexable        bool
	SitemapEligible  bool
	AdultRating      bool
	ImagePreviewMode string
}

type Image struct {
	URL                    string
	Rendition              string
	Width                  int
	Height                 int
	MimeType               string
	Renditions             []ImageRendition
	ExternalPreviewAllowed bool
}

type ImageRendition struct {
	URL                    string `json:"url"`
	Rendition              string `json:"rendition"`
	Width                  int    `json:"width"`
	Height                 int    `json:"height"`
	MimeType               string `json:"mime_type"`
	ExternalPreviewAllowed bool   `json:"external_preview_allowed"`
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
	SearchPerformers(context.Context, string, *SearchCursor, int) ([]PerformerSearchResult, error)
	SearchStudios(context.Context, string, *SearchCursor, int) ([]StudioSearchResult, error)
	WorkBySlug(context.Context, string) (WorkDetail, error)
	ListPerformers(context.Context, string, int) ([]PerformerSummary, error)
	PerformerBySlug(context.Context, string) (PerformerDetail, error)
	PerformerWorksBySlug(context.Context, string, *RelatedWorkCursor, int) ([]RelatedWorkResult, error)
	ListStudios(context.Context, string, int) ([]StudioSummary, error)
	StudioBySlug(context.Context, string) (StudioDetail, error)
	StudioWorksBySlug(context.Context, string, *RelatedWorkCursor, int) ([]RelatedWorkResult, error)
	RecordPageView(context.Context, string, string) error
	RecordSearchOutcome(context.Context, bool) error
	SitemapEntries(context.Context, string, int, int) ([]SitemapEntry, error)
	SitemapCount(context.Context, string) (int, error)
	RedirectTarget(context.Context, string) (RedirectTarget, error)
}
