package operations

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("operations object not found")
	ErrConflict     = errors.New("operations state conflict")
	ErrForbidden    = errors.New("operations action forbidden")
	ErrInvalidInput = errors.New("operations input invalid")
)

type ReviewTask struct {
	ID          string
	TaskType    string
	EntityType  *string
	EntityID    *string
	Priority    int
	Status      string
	AssigneeID  *string
	Assignee    *string
	TargetLabel string
	DueAt       *time.Time
	ClaimedAt   *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ConflictReview struct {
	ID             string
	ReviewTaskID   string
	EntityType     string
	EntityID       string
	FieldName      string
	CurrentValue   any
	CandidateValue any
	SourceID       *string
	SourceName     *string
	SourcePriority *int
	Resolution     *string
	ResolvedBy     *string
	ResolvedAt     *time.Time
	CreatedAt      time.Time
}

type WorkInput struct {
	Code          string
	Title         string
	TitleOriginal *string
	ReleaseDate   *time.Time
	StudioID      *string
	Summary       *string
	PerformerIDs  []string
	Sources       []SourceEvidenceInput
	Reason        string
}

type SourceEvidenceInput struct {
	SourceID    *string
	SourceType  string
	SourceURL   *string
	SourceTitle *string
	CheckedAt   time.Time
}

type SourceEvidence struct {
	FieldName    string    `json:"field_name"`
	SourceType   string    `json:"source_type"`
	SourceURL    *string   `json:"source_url"`
	SourceTitle  *string   `json:"source_title"`
	CheckedAt    time.Time `json:"checked_at"`
	Operator     *string   `json:"operator"`
	Confidence   float64   `json:"confidence"`
	RightsStatus string    `json:"rights_status"`
}

type WorkImportRow struct {
	RowNumber int
	Input     WorkInput
}

type WorkImportInput struct {
	IdempotencyKey string
	RequestHash    string
	Rows           []WorkImportRow
	Reason         string
}

type WorkImportBatch struct {
	ID               string
	Status           string
	InputCount       int
	AcceptedCount    int
	RejectedCount    int
	EntityIDs        []string
	IdempotentReplay bool
	ReceivedAt       time.Time
	CompletedAt      time.Time
}

type StudioInput struct {
	Name          string
	CanonicalSlug string
	Sources       []SourceEvidenceInput
	Reason        string
}

type PerformerInput struct {
	DisplayName    string
	NameOriginal   *string
	RomanizedName  *string
	Aliases        []string
	AdultStatus    string
	ActivityStatus string
	Agency         *string
	BirthYear      *int
	HeightCM       *int
	Measurements   *string
	DebutYear      *int
	Sources        []SourceEvidenceInput
	Reason         string
}

type Entity struct {
	ID             string
	EntityType     string
	CanonicalSlug  string
	Status         string
	RevisionID     string
	RevisionStatus string
	CreatedAt      time.Time
}

type CatalogEntity struct {
	ID                string
	EntityType        string
	Label             string
	Status            string
	CanonicalSlug     *string
	CurrentRevisionID *string
	UpdatedAt         time.Time
}

type Revision struct {
	ID            string
	EntityType    string
	EntityID      string
	Version       int
	Payload       map[string]any
	Status        string
	Author        string
	Reviewer      *string
	Reason        string
	CreatedAt     time.Time
	ReviewedAt    *time.Time
	Sources       []SourceEvidence
	IsCurrent     bool
	CanonicalSlug *string
}

type RevisionInput struct {
	EntityType string
	Payload    map[string]any
	Sources    []SourceEvidenceInput
	Reason     string
}

type PublishInput struct {
	EntityType    string
	RevisionID    string
	CanonicalSlug string
	Reason        string
}

type MergeEntityInput struct {
	EntityType     string
	TargetEntityID string
	Reason         string
}

type EntityMerge struct {
	ID             string
	EntityType     string
	SourceEntityID string
	TargetEntityID string
	SourcePath     string
	TargetPath     string
	MergedAt       time.Time
}

type User struct {
	ID            string
	Email         string
	Role          string
	AccountStatus string
	CreatedAt     time.Time
	LastLoginAt   *time.Time
}

type EditorialRecommendation struct {
	ID        string
	WorkID    string
	WorkCode  string
	WorkTitle string
	WorkSlug  *string
	Position  int
	StartsAt  time.Time
	EndsAt    *time.Time
	Reason    string
	Status    string
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EditorialRecommendationInput struct {
	WorkID       string
	Position     int
	StartsAt     time.Time
	EndsAt       *time.Time
	Reason       string
	ChangeReason string
	Status       string
}

type DiscoveryMixRule struct {
	WorkSlots      int
	PerformerSlots int
	RepeatWindow   int
	Enabled        bool
	UpdatedAt      time.Time
}

type DiscoveryMixRuleInput struct {
	WorkSlots      int
	PerformerSlots int
	RepeatWindow   int
	Enabled        bool
	Reason         string
}

func CanManageUserStatus(actorRole, targetRole string, sameUser bool) bool {
	if sameUser || targetRole == "owner" {
		return false
	}
	return actorRole == "owner" || (actorRole == "admin" && targetRole == "user")
}

// CanManageUserRole describes the narrower role-management boundary. Only an
// owner may change another account's role, and an owner account itself is
// never a valid target. The database repository repeats this check after
// locking the rows so authorization cannot depend on a stale HTTP identity.
func CanManageUserRole(actorRole, targetRole string, sameUser bool) bool {
	return !sameUser && actorRole == "owner" && targetRole != "owner"
}

// ReviewDecisionAccess keeps a claimed review task bound to its assignee.
// Reassignment must be an explicit audited operation instead of an implicit
// side effect of another operator approving the task.
func ReviewDecisionAccess(status, assigneeID, actorID string) error {
	if status != "claimed" || assigneeID == "" {
		return ErrConflict
	}
	if assigneeID != actorID {
		return ErrForbidden
	}
	return nil
}

// CanReceiveReviewTask limits explicit reassignment to active reviewers who
// are authorized to make the eventual decision.
func CanReceiveReviewTask(role, accountStatus string) bool {
	return accountStatus == "active" && (role == "admin" || role == "owner")
}

type SystemHealth struct {
	SchemaVersion             int
	PendingReviewTasks        int64
	ReviewingRevisions        int64
	PendingOutboxEvents       int64
	FailedOutboxEvents        int64
	MediaBytes                int64
	VerifiedBackupAgeSeconds  *float64
	MediaReconcileAgeSeconds  *float64
	MediaReconcileIssues      int64
	FailedMediaReconciles     int64
	MediaInspectionAgeSeconds *float64
	MediaPublicationIssues    int64
	DefaultImageFailures      int64
	FailedMediaInspections    int64
	SearchRequests24h         int64
	SearchZeroResults24h      int64
	CheckedAt                 time.Time
}

type AuditLog struct {
	ID         string
	ActorType  string
	ActorID    *string
	Actor      *string
	Action     string
	ObjectType string
	ObjectID   *string
	Reason     *string
	RequestID  string
	OccurredAt time.Time
}

type MediaObjectInput struct {
	Rendition    string
	StorageScope string
	StorageKey   string
	BackupPath   string
	PublicURL    *string
	SHA256       string
	MimeType     string
	Width        int
	Height       int
	ByteSize     int64
}

type MediaManifestInput struct {
	AssetID     string
	EntityType  string
	EntityID    string
	AssetType   string
	SourceType  string
	SourceURL   *string
	CheckedAt   time.Time
	Confidence  float64
	Purpose     string
	Position    int
	IsPrimary   bool
	Reason      string
	ToolVersion string
	Objects     []MediaObjectInput
}

type MediaManifest struct {
	AssetID       string
	EntityMediaID string
	Version       int
	ObjectCount   int
	Status        string
	CreatedAt     time.Time
}

type PrimaryMedia struct {
	AssetID       string
	EntityMediaID string
	Purpose       string
}

type PrimaryMediaState struct {
	EntityType   string
	EntityID     string
	EntityStatus string
	Primary      *PrimaryMedia
}

type ReplacePrimaryMediaInput struct {
	ExpectedAssetID string
	Manifest        MediaManifestInput
}

type PrimaryMediaReplacement struct {
	Media                MediaManifest
	PreviousAssetID      string
	OldAssetRetired      bool
	PrivateRetainedUntil *time.Time
}

type TakedownInput struct {
	RequesterReference string
	EntityType         string
	EntityID           string
	EvidenceReference  *string
}

type TakedownRequest struct {
	ID                 string
	RequesterReference string
	EntityType         string
	EntityID           string
	EvidenceReference  *string
	Status             string
	AssignedTo         *string
	ReceivedAt         time.Time
	DecidedAt          *time.Time
	CompletedAt        *time.Time
	ResultSummary      *string
	UpdatedAt          time.Time
}

type Repository interface {
	ListReviewTasks(context.Context, string, int) ([]ReviewTask, error)
	ClaimReviewTask(context.Context, string, string, string, time.Time) (ReviewTask, error)
	ReassignReviewTask(context.Context, string, string, string, string, string, time.Time) (ReviewTask, error)
	DecideReviewTask(context.Context, string, string, bool, string, string, time.Time) (ReviewTask, error)
	ListConflictReviews(context.Context, string, int) ([]ConflictReview, error)
	ResolveConflictReview(context.Context, string, string, string, string, string, time.Time) (ConflictReview, error)
	CreateWork(context.Context, WorkInput, string, string, time.Time) (Entity, error)
	ImportWorks(context.Context, WorkImportInput, string, string, time.Time) (WorkImportBatch, error)
	ListWorkImportBatches(context.Context, int) ([]WorkImportBatch, error)
	CreatePerformer(context.Context, PerformerInput, string, string, time.Time) (Entity, error)
	CreateStudio(context.Context, StudioInput, string, string, time.Time) (Entity, error)
	CreateRevision(context.Context, string, RevisionInput, string, string, time.Time) (Entity, error)
	PublishEntity(context.Context, string, PublishInput, string, string, time.Time) (Entity, error)
	HideEntity(context.Context, string, string, string, string, string, time.Time) (Entity, error)
	MergeEntity(context.Context, string, MergeEntityInput, string, string, time.Time) (EntityMerge, error)
	ListEntities(context.Context, string, string, int) ([]CatalogEntity, error)
	ListRevisions(context.Context, string, string, int) ([]Revision, error)
	ListUsers(context.Context, int) ([]User, error)
	ListEditorialRecommendations(context.Context, string, int) ([]EditorialRecommendation, error)
	CreateEditorialRecommendation(context.Context, EditorialRecommendationInput, string, string, time.Time) (EditorialRecommendation, error)
	UpdateEditorialRecommendation(context.Context, string, EditorialRecommendationInput, string, string, time.Time) (EditorialRecommendation, error)
	RemoveEditorialRecommendation(context.Context, string, string, string, string, time.Time) (EditorialRecommendation, error)
	GetDiscoveryMixRule(context.Context) (DiscoveryMixRule, error)
	UpdateDiscoveryMixRule(context.Context, DiscoveryMixRuleInput, string, string, time.Time) (DiscoveryMixRule, error)
	ChangeUserRole(context.Context, string, string, string, string, time.Time) (User, error)
	ChangeUserStatus(context.Context, string, string, string, string, string, string, time.Time) (User, error)
	SystemHealth(context.Context, time.Time) (SystemHealth, error)
	ListAuditLogs(context.Context, string, int) ([]AuditLog, error)
	RegisterMediaManifest(context.Context, MediaManifestInput, string, string, time.Time) (MediaManifest, error)
	GetPrimaryMedia(context.Context, string, string) (PrimaryMediaState, error)
	ReplacePrimaryMedia(context.Context, ReplacePrimaryMediaInput, string, string, time.Time) (PrimaryMediaReplacement, error)
	ListTakedownRequests(context.Context, string, int) ([]TakedownRequest, error)
	CreateTakedownRequest(context.Context, TakedownInput, string, string, time.Time) (TakedownRequest, error)
	CompleteTakedownRequest(context.Context, string, string, string, string, time.Time) (TakedownRequest, error)
}
