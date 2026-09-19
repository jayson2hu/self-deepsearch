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

type SEOScan struct {
	CycleID           string
	BatchCount        int
	TotalScannedCount int
	TotalIssueCount   int
	ID                string
	Source            string
	Status            string
	ScannedCount      int
	IssueCount        int
	SkippedCount      int
	Truncated         bool
	ErrorSummary      *string
	StartedAt         time.Time
	CompletedAt       *time.Time
}

type SEOIssue struct {
	ID             string
	RuleCode       string
	EntityType     string
	EntityID       string
	URLPath        string
	Priority       string
	Status         string
	Evidence       map[string]any
	AssigneeID     *string
	Assignee       *string
	DueAt          *time.Time
	ResolutionNote *string
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	ClosedAt       *time.Time
	UpdatedAt      time.Time
}

type SEOIssueUpdate struct {
	Status     string
	AssigneeID *string
	DueAt      *time.Time
	Note       string
}

type SEOIssueRepository interface {
	RunSEOScan(context.Context, string, string, time.Time) (SEOScan, error)
	ListSEOScans(context.Context, int) ([]SEOScan, error)
	ListSEOIssues(context.Context, string, int) ([]SEOIssue, error)
	UpdateSEOIssue(context.Context, string, SEOIssueUpdate, string, string, time.Time) (SEOIssue, error)
}

type SEOReportRow struct {
	Dimensions map[string]string
	Metrics    map[string]float64
}

type SEOReportImportInput struct {
	ReportType  string
	DatasetKind string
	PropertyURI string
	PeriodStart time.Time
	PeriodEnd   time.Time
	ExportedAt  time.Time
	FileSHA256  string
	Rows        []SEOReportRow
}

type SEOReportImport struct {
	ID           string
	ReportType   string
	DatasetKind  string
	PropertyURI  string
	PeriodStart  time.Time
	PeriodEnd    time.Time
	ExportedAt   time.Time
	FileSHA256   string
	RowCount     int
	Status       string
	ErrorSummary *string
	ImportedBy   string
	CreatedAt    time.Time
}

type SEOReportRepository interface {
	ImportSEOReport(context.Context, SEOReportImportInput, string, string, time.Time) (SEOReportImport, error)
	ListSEOReports(context.Context, int) ([]SEOReportImport, error)
}
