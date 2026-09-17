package operations

import (
	"context"
	"time"
)

type SEOInventorySummary struct {
	ObservedAt        time.Time
	Total             int64
	Indexable         int64
	Excluded          int64
	Unknown           int64
	SitemapCandidates int64
	Published         int64
	Hidden            int64
	Takedown          int64
	Reasons           map[string]int64
}

type SEOPage struct {
	EntityType            string
	EntityID              string
	URLPath               string
	Title                 string
	Description           string
	Canonical             string
	Robots                string
	PublicationStatus     string
	ContentClassification string
	QualityStatus         string
	MediaPolicy           string
	Indexable             bool
	SitemapEligible       bool
	ExclusionReason       *string
	UpdatedAt             time.Time
}

type SEOPageQuery struct {
	EntityType string
	Status     string
	Query      string
	Limit      int
	Offset     int
}

type SEOPageResult struct {
	Items []SEOPage
	Total int64
}

type SEOInventoryRepository interface {
	SEOInventorySummary(context.Context, time.Time) (SEOInventorySummary, error)
	ListSEOPages(context.Context, SEOPageQuery) (SEOPageResult, error)
}
