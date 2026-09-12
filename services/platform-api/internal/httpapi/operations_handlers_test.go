package httpapi

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/operations"
)

type fakeOperations struct {
	tasks          []operations.ReviewTask
	claimActor     string
	reassignActor  string
	reassignTarget string
	reassignReason string
	roleActor      string
	roleValue      string
	statusActor    string
	statusRole     string
	statusValue    string
	operationErr   error
	mergeSource    string
	mergeInput     operations.MergeEntityInput
	takedownActor  string
	conflicts      []operations.ConflictReview
	conflictActor  string
	resolution     string
	importKey      string
	mediaInput     operations.MediaManifestInput
	primaryInput   operations.ReplacePrimaryMediaInput
	primaryState   operations.PrimaryMediaState
	sharedMedia    bool
	performerInput operations.PerformerInput
	revisionInput  operations.RevisionInput
	auditRequestID string
	auditLogs      []operations.AuditLog
}

func (repository *fakeOperations) ListReviewTasks(context.Context, string, int) ([]operations.ReviewTask, error) {
	return repository.tasks, repository.operationErr
}
func (repository *fakeOperations) ClaimReviewTask(_ context.Context, _ string, actorID, _ string, _ time.Time) (operations.ReviewTask, error) {
	repository.claimActor = actorID
	return repository.tasks[0], repository.operationErr
}
func (repository *fakeOperations) ReassignReviewTask(_ context.Context, _ string, assigneeID, actorID, reason, _ string, _ time.Time) (operations.ReviewTask, error) {
	repository.reassignActor, repository.reassignTarget, repository.reassignReason = actorID, assigneeID, reason
	return repository.tasks[0], repository.operationErr
}
func (repository *fakeOperations) DecideReviewTask(context.Context, string, string, bool, string, string, time.Time) (operations.ReviewTask, error) {
	return repository.tasks[0], repository.operationErr
}
func (repository *fakeOperations) ListConflictReviews(context.Context, string, int) ([]operations.ConflictReview, error) {
	return repository.conflicts, repository.operationErr
}
func (repository *fakeOperations) ResolveConflictReview(_ context.Context, id, resolution, actorID, _ string, _ string, now time.Time) (operations.ConflictReview, error) {
	repository.conflictActor, repository.resolution = actorID, resolution
	return operations.ConflictReview{ID: id, ReviewTaskID: "10000000-0000-4000-8000-000000000002", EntityType: "work",
		EntityID: "10000000-0000-4000-8000-000000000001", FieldName: "release_date", Resolution: &resolution,
		ResolvedAt: &now, CreatedAt: now}, repository.operationErr
}
func (repository *fakeOperations) CreateWork(context.Context, operations.WorkInput, string, string, time.Time) (operations.Entity, error) {
	return operations.Entity{}, repository.operationErr
}
func (repository *fakeOperations) ImportWorks(_ context.Context, input operations.WorkImportInput, _ string, _ string, now time.Time) (operations.WorkImportBatch, error) {
	repository.importKey = input.IdempotencyKey
	return operations.WorkImportBatch{ID: "42000000-0000-4000-8000-000000000001", Status: "accepted", InputCount: len(input.Rows),
		AcceptedCount: len(input.Rows), EntityIDs: []string{"10000000-0000-4000-8000-000000000010"}, ReceivedAt: now, CompletedAt: now}, repository.operationErr
}
func (repository *fakeOperations) ListWorkImportBatches(context.Context, int) ([]operations.WorkImportBatch, error) {
	return []operations.WorkImportBatch{}, repository.operationErr
}
func (repository *fakeOperations) CreatePerformer(_ context.Context, input operations.PerformerInput, _ string, _ string, _ time.Time) (operations.Entity, error) {
	repository.performerInput = input
	return operations.Entity{}, repository.operationErr
}
func (repository *fakeOperations) CreateStudio(context.Context, operations.StudioInput, string, string, time.Time) (operations.Entity, error) {
	return operations.Entity{}, repository.operationErr
}
func (repository *fakeOperations) CreateRevision(_ context.Context, _ string, input operations.RevisionInput, _ string, _ string, _ time.Time) (operations.Entity, error) {
	repository.revisionInput = input
	return operations.Entity{}, repository.operationErr
}
func (repository *fakeOperations) PublishEntity(context.Context, string, operations.PublishInput, string, string, time.Time) (operations.Entity, error) {
	return operations.Entity{}, repository.operationErr
}
func (repository *fakeOperations) HideEntity(context.Context, string, string, string, string, string, time.Time) (operations.Entity, error) {
	return operations.Entity{}, repository.operationErr
}
func (repository *fakeOperations) MergeEntity(_ context.Context, sourceID string, input operations.MergeEntityInput, _ string, _ string, now time.Time) (operations.EntityMerge, error) {
	repository.mergeSource, repository.mergeInput = sourceID, input
	return operations.EntityMerge{ID: "70000000-0000-4000-8000-000000000003", EntityType: input.EntityType,
		SourceEntityID: sourceID, TargetEntityID: input.TargetEntityID, SourcePath: "/works/old",
		TargetPath: "/works/new", MergedAt: now}, repository.operationErr
}
func (repository *fakeOperations) ListEntities(context.Context, string, string, int) ([]operations.CatalogEntity, error) {
	return []operations.CatalogEntity{}, repository.operationErr
}
func (repository *fakeOperations) ListRevisions(context.Context, string, string, int) ([]operations.Revision, error) {
	return []operations.Revision{}, repository.operationErr
}
func (repository *fakeOperations) ListUsers(context.Context, int) ([]operations.User, error) {
	return []operations.User{}, repository.operationErr
}
func (repository *fakeOperations) ListEditorialRecommendations(context.Context, string, int) ([]operations.EditorialRecommendation, error) {
	return []operations.EditorialRecommendation{}, repository.operationErr
}
func (repository *fakeOperations) CreateEditorialRecommendation(_ context.Context, input operations.EditorialRecommendationInput, _ string, _ string, now time.Time) (operations.EditorialRecommendation, error) {
	return operations.EditorialRecommendation{ID: "70000000-0000-4000-8000-000000000001", WorkID: input.WorkID,
		WorkCode: "TEST-001", WorkTitle: "测试作品", Position: input.Position, StartsAt: input.StartsAt,
		EndsAt: input.EndsAt, Reason: input.Reason, Status: input.Status, CreatedBy: "operator@example.test",
		CreatedAt: now, UpdatedAt: now}, repository.operationErr
}
func (repository *fakeOperations) UpdateEditorialRecommendation(ctx context.Context, _ string, input operations.EditorialRecommendationInput, actorID, requestID string, now time.Time) (operations.EditorialRecommendation, error) {
	return repository.CreateEditorialRecommendation(ctx, input, actorID, requestID, now)
}
func (repository *fakeOperations) RemoveEditorialRecommendation(_ context.Context, id, _ string, reason, _ string, now time.Time) (operations.EditorialRecommendation, error) {
	return operations.EditorialRecommendation{ID: id, WorkID: "10000000-0000-4000-8000-000000000001",
		WorkCode: "TEST-001", WorkTitle: "测试作品", Position: 1, StartsAt: now, Reason: reason,
		Status: "removed", CreatedBy: "operator@example.test", CreatedAt: now, UpdatedAt: now}, repository.operationErr
}
func (repository *fakeOperations) GetDiscoveryMixRule(context.Context) (operations.DiscoveryMixRule, error) {
	return operations.DiscoveryMixRule{WorkSlots: 4, PerformerSlots: 1, RepeatWindow: 10, Enabled: true}, repository.operationErr
}
func (repository *fakeOperations) UpdateDiscoveryMixRule(_ context.Context, input operations.DiscoveryMixRuleInput, _ string, _ string, now time.Time) (operations.DiscoveryMixRule, error) {
	return operations.DiscoveryMixRule{WorkSlots: input.WorkSlots, PerformerSlots: input.PerformerSlots,
		RepeatWindow: input.RepeatWindow, Enabled: input.Enabled, UpdatedAt: now}, repository.operationErr
}
func (repository *fakeOperations) ChangeUserRole(_ context.Context, _ string, role, actorID, _ string, _ time.Time) (operations.User, error) {
	repository.roleActor, repository.roleValue = actorID, role
	return operations.User{ID: "10000000-0000-4000-8000-000000000001", Role: role, CreatedAt: time.Now()}, repository.operationErr
}
func (repository *fakeOperations) ChangeUserStatus(_ context.Context, _ string, status, actorID, actorRole, _ string, _ string, _ time.Time) (operations.User, error) {
	repository.statusActor, repository.statusRole, repository.statusValue = actorID, actorRole, status
	return operations.User{ID: "10000000-0000-4000-8000-000000000001", Role: "user", AccountStatus: status, CreatedAt: time.Now()}, repository.operationErr
}
func (repository *fakeOperations) SystemHealth(context.Context, time.Time) (operations.SystemHealth, error) {
	return operations.SystemHealth{SchemaVersion: requiredDatabaseSchemaVersion}, repository.operationErr
}
func (repository *fakeOperations) ListAuditLogs(_ context.Context, requestID string, _ int) ([]operations.AuditLog, error) {
	repository.auditRequestID = requestID
	return repository.auditLogs, repository.operationErr
}
func (repository *fakeOperations) RegisterMediaManifest(_ context.Context, input operations.MediaManifestInput, _ string, _ string, now time.Time) (operations.MediaManifest, error) {
	repository.mediaInput = input
	return operations.MediaManifest{AssetID: input.AssetID, EntityMediaID: "30000000-0000-4000-8000-000000000001", Version: 1, ObjectCount: len(input.Objects), Status: "published", CreatedAt: now}, repository.operationErr
}

func (repository *fakeOperations) GetPrimaryMedia(context.Context, string, string) (operations.PrimaryMediaState, error) {
	return repository.primaryState, repository.operationErr
}

func (repository *fakeOperations) ReplacePrimaryMedia(ctx context.Context, input operations.ReplacePrimaryMediaInput, actorID, requestID string, now time.Time) (operations.PrimaryMediaReplacement, error) {
	repository.primaryInput = input
	media, err := repository.RegisterMediaManifest(ctx, input.Manifest, actorID, requestID, now)
	result := operations.PrimaryMediaReplacement{Media: media, PreviousAssetID: input.ExpectedAssetID, OldAssetRetired: !repository.sharedMedia}
	if result.OldAssetRetired {
		deadline := now.Add(30 * 24 * time.Hour)
		result.PrivateRetainedUntil = &deadline
	}
	return result, err
}
func (repository *fakeOperations) ListTakedownRequests(context.Context, string, int) ([]operations.TakedownRequest, error) {
	return []operations.TakedownRequest{}, repository.operationErr
}
func (repository *fakeOperations) CreateTakedownRequest(_ context.Context, input operations.TakedownInput, actorID, _ string, now time.Time) (operations.TakedownRequest, error) {
	repository.takedownActor = actorID
	return operations.TakedownRequest{ID: "10000000-0000-4000-8000-000000000010", RequesterReference: input.RequesterReference, EntityType: input.EntityType, EntityID: input.EntityID, Status: "received", ReceivedAt: now, UpdatedAt: now}, repository.operationErr
}
func (repository *fakeOperations) CompleteTakedownRequest(_ context.Context, id, actorID, summary, _ string, now time.Time) (operations.TakedownRequest, error) {
	repository.takedownActor = actorID
	return operations.TakedownRequest{ID: id, RequesterReference: "email", EntityType: "work", EntityID: "10000000-0000-4000-8000-000000000001", Status: "completed", ResultSummary: &summary, ReceivedAt: now, UpdatedAt: now}, repository.operationErr
}

func adminHandler(role string, repository operations.Repository) http.Handler {
	return New(Options{
		Identity:   &fakeIdentityService{user: identity.User{ID: "90000000-0000-4000-8000-000000000001", Email: "operator@example.test", Role: role}},
		Operations: repository,
	})
}

func authenticatedAdminRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "session"})
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func withSourceEvidence(body string) string {
	return strings.TrimSuffix(body, "}") + `,"sources":[{"source_type":"web_page","source_url":"https://example.test/source","source_title":"Example source","checked_at":"2026-08-01T12:00:00Z"}]}`
}

func TestAdminRoutesRejectNormalUsers(t *testing.T) {
	handler := adminHandler("user", &fakeOperations{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodGet, "/admin/v1/review-tasks", ""))
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}

func TestEditorCanListButCannotClaimReviewTask(t *testing.T) {
	repository := &fakeOperations{tasks: []operations.ReviewTask{{
		ID: "10000000-0000-4000-8000-000000000001", TaskType: "publication", Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}}
	handler := adminHandler("editor", repository)

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, authenticatedAdminRequest(http.MethodGet, "/admin/v1/review-tasks", ""))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", listResponse.Code, listResponse.Body.String())
	}

	claimResponse := httptest.NewRecorder()
	handler.ServeHTTP(claimResponse, authenticatedAdminRequest(http.MethodPost, "/admin/v1/review-tasks/10000000-0000-4000-8000-000000000001/claim", ""))
	if claimResponse.Code != http.StatusForbidden || repository.claimActor != "" {
		t.Fatalf("expected editor claim 403, got %d: %s", claimResponse.Code, claimResponse.Body.String())
	}
}

func TestAdminCanClaimReviewTask(t *testing.T) {
	repository := &fakeOperations{tasks: []operations.ReviewTask{{
		ID: "10000000-0000-4000-8000-000000000001", TaskType: "publication", Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}}
	handler := adminHandler("admin", repository)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/review-tasks/10000000-0000-4000-8000-000000000001/claim", ""))
	if response.Code != http.StatusOK || repository.claimActor == "" {
		t.Fatalf("expected admin claim 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestAdminCanExplicitlyReassignClaimedReviewTask(t *testing.T) {
	const targetID = "10000000-0000-4000-8000-000000000002"
	repository := &fakeOperations{tasks: []operations.ReviewTask{{
		ID: "10000000-0000-4000-8000-000000000001", TaskType: "publication", Status: "claimed", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}}
	response := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/review-tasks/10000000-0000-4000-8000-000000000001/reassign",
		`{"assignee_id":"`+targetID+`","reason":"reviewer handoff"}`))
	if response.Code != http.StatusOK || repository.reassignTarget != targetID || repository.reassignActor == "" || repository.reassignReason != "reviewer handoff" {
		t.Fatalf("expected audited reassignment request, got %d: %s", response.Code, response.Body.String())
	}
}

func TestReviewTaskReassignmentValidatesRoleAndInput(t *testing.T) {
	const path = "/admin/v1/review-tasks/10000000-0000-4000-8000-000000000001/reassign"
	for _, test := range []struct {
		name string
		role string
		body string
		want int
	}{
		{name: "editor forbidden", role: "editor", body: `{"assignee_id":"10000000-0000-4000-8000-000000000002","reason":"handoff"}`, want: http.StatusForbidden},
		{name: "invalid assignee", role: "admin", body: `{"assignee_id":"not-a-uuid","reason":"handoff"}`, want: http.StatusBadRequest},
		{name: "short reason", role: "owner", body: `{"assignee_id":"10000000-0000-4000-8000-000000000002","reason":"x"}`, want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeOperations{tasks: []operations.ReviewTask{{ID: "10000000-0000-4000-8000-000000000001"}}}
			response := httptest.NewRecorder()
			adminHandler(test.role, repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, path, test.body))
			if response.Code != test.want || repository.reassignActor != "" {
				t.Fatalf("expected %d before persistence, got %d: %s", test.want, response.Code, response.Body.String())
			}
		})
	}
}

func TestEditorCannotApproveReviewTask(t *testing.T) {
	repository := &fakeOperations{tasks: []operations.ReviewTask{{ID: "10000000-0000-4000-8000-000000000001"}}}
	handler := adminHandler("editor", repository)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/review-tasks/10000000-0000-4000-8000-000000000001/approve", `{"reason":"checked"}`))
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}

func TestOnlyOwnerCanChangeRoles(t *testing.T) {
	repository := &fakeOperations{}
	adminResponse := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(adminResponse, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/users/10000000-0000-4000-8000-000000000001/role", `{"role":"editor"}`))
	if adminResponse.Code != http.StatusForbidden {
		t.Fatalf("expected admin to receive 403, got %d", adminResponse.Code)
	}

	ownerResponse := httptest.NewRecorder()
	adminHandler("owner", repository).ServeHTTP(ownerResponse, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/users/10000000-0000-4000-8000-000000000001/role", `{"role":"editor"}`))
	if ownerResponse.Code != http.StatusOK || repository.roleValue != "editor" || repository.roleActor == "" {
		t.Fatalf("expected owner role update, got %d: %s", ownerResponse.Code, ownerResponse.Body.String())
	}
}

func TestRoleChangeFailureDoesNotReturnSuccessOrCookies(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{operations.ErrConflict, http.StatusConflict},
		{errors.New("synthetic revocation failure"), http.StatusServiceUnavailable},
	} {
		repository := &fakeOperations{operationErr: test.err}
		response := httptest.NewRecorder()
		adminHandler("owner", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
			"/admin/v1/users/10000000-0000-4000-8000-000000000001/role", `{"role":"editor"}`))
		if response.Code != test.status || len(response.Result().Cookies()) != 0 || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") || strings.Contains(response.Body.String(), "synthetic") {
			t.Fatalf("role mutation failure exposed success or internals: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestRevokedSessionCannotUseOldRecentAuthForRoleChange(t *testing.T) {
	repository := &fakeOperations{}
	service := &fakeRecentIdentityService{fakeIdentityService: &fakeIdentityService{err: identity.ErrUnauthenticated}, validateErr: nil}
	handler := New(Options{Identity: service, Operations: repository})
	request := authenticatedAdminRequest(http.MethodPost, "/admin/v1/users/10000000-0000-4000-8000-000000000001/role", `{"role":"editor"}`)
	request.AddCookie(&http.Cookie{Name: identity.ReauthCookieName, Value: "otherwise-valid-recent-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || repository.roleActor != "" {
		t.Fatal("recent-auth token bypassed revoked login session")
	}
}

func TestAdminAndOwnerCanRequestAccountStatusChanges(t *testing.T) {
	for _, role := range []string{"admin", "owner"} {
		repository := &fakeOperations{}
		response := httptest.NewRecorder()
		adminHandler(role, repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
			"/admin/v1/users/10000000-0000-4000-8000-000000000001/status", `{"status":"suspended","reason":"manual moderation"}`))
		if response.Code != http.StatusOK || repository.statusValue != "suspended" || repository.statusRole != role || repository.statusActor == "" {
			t.Fatalf("expected %s status update, got %d: %s", role, response.Code, response.Body.String())
		}
	}

	editorResponse := httptest.NewRecorder()
	adminHandler("editor", &fakeOperations{}).ServeHTTP(editorResponse, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/users/10000000-0000-4000-8000-000000000001/status", `{"status":"locked","reason":"manual moderation"}`))
	if editorResponse.Code != http.StatusForbidden {
		t.Fatalf("expected editor to receive 403, got %d", editorResponse.Code)
	}
}

func TestCreateWorkValidatesReleaseDate(t *testing.T) {
	handler := adminHandler("editor", &fakeOperations{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/works",
		`{"code":"TEST-010","title":"Test","release_date":"08/01/2026","reason":"manual entry"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}

func TestCreateWorkRejectsDuplicatePerformerIDs(t *testing.T) {
	handler := adminHandler("editor", &fakeOperations{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/works",
		`{"code":"TEST-010","title":"Test","performer_ids":["10000000-0000-4000-8000-000000000001","10000000-0000-4000-8000-000000000001"],"reason":"manual entry"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}

func TestCreatePerformerNormalizesAliases(t *testing.T) {
	repository := &fakeOperations{}
	response := httptest.NewRecorder()
	adminHandler("editor", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/performers",
		withSourceEvidence(`{"display_name":"Test Person","aliases":[" Alias  One ","Alias Two"],"reason":"manual entry"}`)))
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
	want := []string{"Alias One", "Alias Two"}
	if len(repository.performerInput.Aliases) != len(want) || repository.performerInput.Aliases[0] != want[0] || repository.performerInput.Aliases[1] != want[1] {
		t.Fatalf("unexpected normalized aliases: %#v", repository.performerInput.Aliases)
	}
	if len(repository.performerInput.Sources) != 1 || repository.performerInput.Sources[0].SourceType != "web_page" {
		t.Fatalf("source evidence was not passed to persistence: %#v", repository.performerInput.Sources)
	}
}

func TestCreatePerformerRequiresSourceEvidence(t *testing.T) {
	response := httptest.NewRecorder()
	adminHandler("editor", &fakeOperations{}).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/performers",
		`{"display_name":"Test Person","reason":"manual entry"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected missing source evidence to be rejected, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCreatePerformerAcceptsSourceWithoutURL(t *testing.T) {
	repository := &fakeOperations{}
	response := httptest.NewRecorder()
	adminHandler("editor", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/performers",
		`{"display_name":"Test Person","reason":"manual entry","sources":[{"source_type":"search_result","source_url":null,"source_title":null,"checked_at":"2026-08-01T12:00:00Z"}]}`))
	if response.Code != http.StatusCreated || len(repository.performerInput.Sources) != 1 || repository.performerInput.Sources[0].SourceURL != nil {
		t.Fatalf("expected source evidence without URL to be accepted, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCreatePerformerRejectsNonHTTPSourceURL(t *testing.T) {
	response := httptest.NewRecorder()
	adminHandler("editor", &fakeOperations{}).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/performers",
		`{"display_name":"Test Person","reason":"manual entry","sources":[{"source_type":"web_page","source_url":"ftp://example.test/source","checked_at":"2026-08-01T12:00:00Z"}]}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected non-HTTP source URL to be rejected, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCreatePerformerRejectsNormalizedDuplicateAliases(t *testing.T) {
	repository := &fakeOperations{}
	response := httptest.NewRecorder()
	adminHandler("editor", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/performers",
		`{"display_name":"Test Person","aliases":["Alias One"," alias   one "],"reason":"manual entry"}`))
	if response.Code != http.StatusBadRequest || len(repository.performerInput.Aliases) != 0 {
		t.Fatalf("expected duplicate aliases to be rejected before persistence, got %d: %s", response.Code, response.Body.String())
	}
}

func TestEditorCanReadCatalogAndVersions(t *testing.T) {
	handler := adminHandler("editor", &fakeOperations{})
	for _, target := range []string{
		"/admin/v1/entities?entity_type=work&status=published",
		"/admin/v1/entities/10000000-0000-4000-8000-000000000001/revisions?entity_type=work",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodGet, target, ""))
		if response.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d: %s", target, response.Code, response.Body.String())
		}
	}
}

func TestEntityMergeRequiresAdminOrOwner(t *testing.T) {
	const sourceID = "10000000-0000-4000-8000-000000000001"
	const targetID = "10000000-0000-4000-8000-000000000002"
	body := `{"entity_type":"work","target_entity_id":"` + targetID + `","reason":"duplicate record"}`

	editorResponse := httptest.NewRecorder()
	adminHandler("editor", &fakeOperations{}).ServeHTTP(editorResponse, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/entities/"+sourceID+"/merge", body))
	if editorResponse.Code != http.StatusForbidden {
		t.Fatalf("expected editor to receive 403, got %d", editorResponse.Code)
	}

	for _, role := range []string{"admin", "owner"} {
		repository := &fakeOperations{}
		response := httptest.NewRecorder()
		adminHandler(role, repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
			"/admin/v1/entities/"+sourceID+"/merge", body))
		if response.Code != http.StatusOK || repository.mergeSource != sourceID || repository.mergeInput.TargetEntityID != targetID {
			t.Fatalf("expected %s merge to succeed, got %d: %s", role, response.Code, response.Body.String())
		}
	}
}

func TestEntityMergeRejectsSameSourceAndTarget(t *testing.T) {
	const entityID = "10000000-0000-4000-8000-000000000001"
	repository := &fakeOperations{}
	response := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/entities/"+entityID+"/merge",
		`{"entity_type":"work","target_entity_id":"`+entityID+`","reason":"duplicate record"}`))
	if response.Code != http.StatusBadRequest || repository.mergeSource != "" {
		t.Fatalf("expected same-entity merge to be rejected before persistence, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRevisionRejectsWrongFieldTypes(t *testing.T) {
	handler := adminHandler("editor", &fakeOperations{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/entities/10000000-0000-4000-8000-000000000001/revisions",
		`{"entity_type":"performer","payload":{"birth_year":"not-a-year"},"reason":"manual correction"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRevisionAcceptsOptionalNullsAndPerformerIDs(t *testing.T) {
	repository := &fakeOperations{}
	handler := adminHandler("editor", repository)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/entities/10000000-0000-4000-8000-000000000001/revisions",
		withSourceEvidence(`{"entity_type":"work","payload":{"canonical_code":"TEST-001","title":"Updated title","title_original":null,"release_date":null,"studio_id":null,"summary":null,"performer_ids":["10000000-0000-4000-8000-000000000002"]},"reason":"clear stale optional fields"}`)))
	if response.Code != http.StatusCreated {
		t.Fatalf("expected optional nulls and performer IDs to be accepted, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRevisionAcceptsPerformerAliases(t *testing.T) {
	repository := &fakeOperations{}
	response := httptest.NewRecorder()
	adminHandler("editor", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/entities/10000000-0000-4000-8000-000000000001/revisions",
		withSourceEvidence(`{"entity_type":"performer","payload":{"aliases":[" Alias  One ","Alias Two"]},"reason":"update aliases"}`)))
	if response.Code != http.StatusCreated {
		t.Fatalf("expected alias revision to be accepted, got %d: %s", response.Code, response.Body.String())
	}
	aliases, ok := repository.revisionInput.Payload["aliases"].([]any)
	if !ok || len(aliases) != 2 || aliases[0] != "Alias One" || aliases[1] != "Alias Two" {
		t.Fatalf("unexpected revision aliases: %#v", repository.revisionInput.Payload["aliases"])
	}
	if len(repository.revisionInput.Sources) != 1 {
		t.Fatalf("revision source evidence was not preserved: %#v", repository.revisionInput.Sources)
	}
}

func TestRevisionRejectsFutureSourceCheckTime(t *testing.T) {
	response := httptest.NewRecorder()
	adminHandler("editor", &fakeOperations{}).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/entities/10000000-0000-4000-8000-000000000001/revisions",
		`{"entity_type":"performer","payload":{"display_name":"Updated"},"reason":"update name","sources":[{"source_type":"search_result","checked_at":"2999-01-01T00:00:00Z"}]}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected future source check time to be rejected, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRevisionRejectsNormalizedDuplicatePerformerAliases(t *testing.T) {
	repository := &fakeOperations{}
	response := httptest.NewRecorder()
	adminHandler("editor", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/entities/10000000-0000-4000-8000-000000000001/revisions",
		`{"entity_type":"performer","payload":{"aliases":["Alias One"," alias   one "]},"reason":"update aliases"}`))
	if response.Code != http.StatusBadRequest || repository.revisionInput.Payload != nil {
		t.Fatalf("expected duplicate aliases to be rejected before persistence, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRevisionRejectsNullRequiredField(t *testing.T) {
	handler := adminHandler("editor", &fakeOperations{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/entities/10000000-0000-4000-8000-000000000001/revisions",
		`{"entity_type":"performer","payload":{"display_name":null},"reason":"invalid empty name"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected null required field to be rejected, got %d: %s", response.Code, response.Body.String())
	}
}

func TestMediaManifestRequiresBackupAndHTTPSPublicURL(t *testing.T) {
	handler := adminHandler("admin", &fakeOperations{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/media/manifests", `{
  "entity_type":"work","entity_id":"10000000-0000-4000-8000-000000000001",
  "asset_type":"work_image","source_type":"manual","checked_at":"2026-08-09T00:00:00Z",
  "confidence":1,"purpose":"cover","position":0,"is_primary":true,"reason":"manual image review",
  "objects":[{"rendition":"w640","storage_key":"works/test/w640.webp","backup_path":"",
    "public_url":"http://cdn.example.test/w640.webp","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "mime_type":"image/webp","width":640,"height":400,"byte_size":1000}]
}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
	}
}

const processorMediaManifestJSON = `{
  "asset_id":"20000000-0000-4000-8000-000000000001",
  "entity_type":"work","entity_id":"10000000-0000-4000-8000-000000000001",
  "asset_type":"work_image","source_type":"manual","source_url":null,"checked_at":"2026-08-18T00:00:00Z",
  "confidence":1,"purpose":"cover","position":0,"is_primary":true,"reason":"manual image review","tool_version":"media-python/1",
  "objects":[
    {"rendition":"master","storage_scope":"private","storage_key":"media-master/20000000-0000-4000-8000-000000000001/v1/master.webp","backup_path":"media-master/20000000-0000-4000-8000-000000000001/v1/master.webp","public_url":null,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","mime_type":"image/webp","width":960,"height":600,"byte_size":1200},
    {"rendition":"w320","storage_scope":"public","storage_key":"media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w320.webp","backup_path":"media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w320.webp","public_url":"https://media.example.test/media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w320.webp","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","mime_type":"image/webp","width":320,"height":200,"byte_size":400},
    {"rendition":"w640","storage_scope":"public","storage_key":"media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w640.webp","backup_path":"media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w640.webp","public_url":"https://media.example.test/media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w640.webp","sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","mime_type":"image/webp","width":640,"height":400,"byte_size":800},
    {"rendition":"w960","storage_scope":"public","storage_key":"media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w960.webp","backup_path":"media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w960.webp","public_url":"https://media.example.test/media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w960.webp","sha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","mime_type":"image/webp","width":960,"height":600,"byte_size":1000}
  ]
}`

func TestMediaManifestAcceptsProcessorGeneratedMasterAndDerivatives(t *testing.T) {
	repository := &fakeOperations{}
	handler := adminHandler("admin", repository)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/media/manifests", processorMediaManifestJSON))
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
	if repository.mediaInput.AssetID == "" || len(repository.mediaInput.Objects) != 4 || repository.mediaInput.Objects[0].PublicURL != nil {
		t.Fatalf("processor manifest was not preserved: %#v", repository.mediaInput)
	}
}

func TestMediaManifestFailuresNeverReturnPublishedResult(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"withdrawn_or_duplicate", operations.ErrConflict, http.StatusConflict, "STATE_CONFLICT"},
		{"missing_parent", operations.ErrNotFound, http.StatusNotFound, "OPERATIONS_OBJECT_NOT_FOUND"},
		{"outbox_or_commit_failure", errors.New("synthetic private database diagnostic"), http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repository := &fakeOperations{operationErr: scenario.err}
			response := httptest.NewRecorder()
			adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
				"/admin/v1/media/manifests", processorMediaManifestJSON))
			body := response.Body.String()
			if response.Code != scenario.status || !strings.Contains(body, scenario.code) {
				t.Fatalf("unexpected failure response: %d %s", response.Code, body)
			}
			if repository.mediaInput.AssetID == "" || strings.Contains(body, `"asset_id"`) || strings.Contains(body, "published") || strings.Contains(body, "diagnostic") {
				t.Fatal("failure either bypassed repository or exposed a successful manifest / internal details")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("media write failure must not be cached")
			}
		})
	}
}

func TestEditorCannotCreateTakedownRequest(t *testing.T) {
	response := httptest.NewRecorder()
	adminHandler("editor", &fakeOperations{}).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/takedowns", `{"requester_reference":"rights@example.test","entity_type":"work","entity_id":"10000000-0000-4000-8000-000000000001"}`))
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}

func TestAdminCanRegisterAndCompleteTakedown(t *testing.T) {
	repository := &fakeOperations{}
	handler := adminHandler("admin", repository)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, authenticatedAdminRequest(http.MethodPost, "/admin/v1/takedowns",
		`{"requester_reference":"rights@example.test","entity_type":"work","entity_id":"10000000-0000-4000-8000-000000000001","evidence_reference":"mailbox case 42"}`))
	if created.Code != http.StatusCreated || repository.takedownActor == "" {
		t.Fatalf("expected 201, got %d: %s", created.Code, created.Body.String())
	}
	completed := httptest.NewRecorder()
	handler.ServeHTTP(completed, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/takedowns/10000000-0000-4000-8000-000000000010/complete", `{"result_summary":"rights request verified"}`))
	if completed.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", completed.Code, completed.Body.String())
	}
}

func TestTakedownDeletionFailureNeverReturnsCompletedResult(t *testing.T) {
	repository := &fakeOperations{operationErr: errors.New("synthetic private deletion identity diagnostic")}
	response := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/takedowns/10000000-0000-4000-8000-000000000010/complete", `{"result_summary":"rights request verified"}`))
	body := response.Body.String()
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(body, "OPERATIONS_UNAVAILABLE") ||
		strings.Contains(body, "completed") || strings.Contains(body, "diagnostic") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("deletion failure falsely reported completion or leaked details: %d %s", response.Code, body)
	}
}

func TestEditorCanListButCannotResolveConflicts(t *testing.T) {
	repository := &fakeOperations{conflicts: []operations.ConflictReview{{
		ID: "10000000-0000-4000-8000-000000000010", ReviewTaskID: "10000000-0000-4000-8000-000000000002",
		EntityType: "work", EntityID: "10000000-0000-4000-8000-000000000001", FieldName: "release_date", CreatedAt: time.Now(),
	}}}
	handler := adminHandler("editor", repository)
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, authenticatedAdminRequest(http.MethodGet, "/admin/v1/conflicts?status=open", ""))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"field_name":"release_date"`) {
		t.Fatalf("expected editor conflict list, got %d: %s", listed.Code, listed.Body.String())
	}
	resolved := httptest.NewRecorder()
	handler.ServeHTTP(resolved, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/conflicts/10000000-0000-4000-8000-000000000010/resolve",
		`{"resolution":"keep_current","reason":"manual comparison"}`))
	if resolved.Code != http.StatusForbidden {
		t.Fatalf("expected editor resolve to receive 403, got %d", resolved.Code)
	}
}

func TestAdminCanResolveConflict(t *testing.T) {
	repository := &fakeOperations{}
	response := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/conflicts/10000000-0000-4000-8000-000000000010/resolve",
		`{"resolution":"accept_candidate","reason":"source evidence reviewed"}`))
	if response.Code != http.StatusOK || repository.conflictActor == "" || repository.resolution != "accept_candidate" {
		t.Fatalf("expected admin conflict resolution, got %d: %s", response.Code, response.Body.String())
	}
}

func TestEditorCanPreflightWorkCSV(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "works.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("code,title,release_date\nAB-123,Test,2026-08-01\nAB123,Duplicate,not-a-date\n"))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/admin/v1/imports/works/preflight", &body)
	request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "session"})
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	adminHandler("editor", &fakeOperations{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"valid_count":1`) || !strings.Contains(response.Body.String(), `"invalid_count":1`) {
		t.Fatalf("expected CSV report, got %d: %s", response.Code, response.Body.String())
	}
}

func TestEditorCanCommitValidWorkCSV(t *testing.T) {
	repository := &fakeOperations{}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, _ := writer.CreateFormFile("file", "works.csv")
	_, _ = file.Write([]byte("code,title,release_date\nAB-124,Test,2026-08-01\n"))
	_ = writer.WriteField("reason", "initial catalog import")
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/admin/v1/imports/works", &body)
	request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "session"})
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Idempotency-Key", "import-test-001")
	response := httptest.NewRecorder()
	adminHandler("editor", repository).ServeHTTP(response, request)
	if response.Code != http.StatusCreated || repository.importKey != "import-test-001" {
		t.Fatalf("expected committed CSV batch, got %d: %s", response.Code, response.Body.String())
	}
}

func TestEditorCannotManageEditorialRecommendations(t *testing.T) {
	response := httptest.NewRecorder()
	adminHandler("editor", &fakeOperations{}).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet,
		"/admin/v1/editorial-recommendations", ""))
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestEditorialRecommendationRejectsInvalidTimeRange(t *testing.T) {
	response := httptest.NewRecorder()
	adminHandler("admin", &fakeOperations{}).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/editorial-recommendations", `{"work_id":"10000000-0000-4000-8000-000000000001","position":1,"starts_at":"2026-08-20T00:00:00Z","ends_at":"2026-08-19T00:00:00Z","reason":"manual recommendation","change_reason":"create schedule","status":"active"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestAdminCanCreateEditorialRecommendation(t *testing.T) {
	response := httptest.NewRecorder()
	adminHandler("admin", &fakeOperations{}).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost,
		"/admin/v1/editorial-recommendations", `{"work_id":"10000000-0000-4000-8000-000000000001","position":1,"starts_at":"2026-08-19T00:00:00Z","ends_at":null,"reason":"manual recommendation","change_reason":"create schedule","status":"active"}`))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"status":"active"`) {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
}

func TestAdminCanListMinimalAuditTimelineByRequestID(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 30, 0, 0, time.UTC)
	actorID := "40000000-0000-4000-8000-000000000002"
	actor := "operator@example.test"
	objectID := "10000000-0000-4000-8000-000000000001"
	reason := "manual publication review"
	repository := &fakeOperations{auditLogs: []operations.AuditLog{{
		ID: "42", ActorType: "user", ActorID: &actorID, Actor: &actor, Action: "entity.publish",
		ObjectType: "work", ObjectID: &objectID, Reason: &reason, RequestID: "release-a:publish-42", OccurredAt: now,
	}}}
	response := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet,
		"/admin/v1/audit-logs?request_id=release-a%3Apublish-42", ""))
	body := response.Body.String()
	if response.Code != http.StatusOK || repository.auditRequestID != "release-a:publish-42" ||
		!strings.Contains(body, `"action":"entity.publish"`) || !strings.Contains(body, `"actor":"operator@example.test"`) {
		t.Fatalf("expected audit timeline, got %d: %s", response.Code, body)
	}
	if strings.Contains(body, "before_version") || strings.Contains(body, "after_version") || strings.Contains(body, "metadata") {
		t.Fatalf("audit response exposed raw payload fields: %s", body)
	}
}

func TestAuditTimelineRequiresAdminAndValidRequestID(t *testing.T) {
	editorResponse := httptest.NewRecorder()
	adminHandler("editor", &fakeOperations{}).ServeHTTP(editorResponse, authenticatedAdminRequest(http.MethodGet,
		"/admin/v1/audit-logs?request_id=release-a%3Apublish-42", ""))
	if editorResponse.Code != http.StatusForbidden {
		t.Fatalf("expected editor to receive 403, got %d: %s", editorResponse.Code, editorResponse.Body.String())
	}

	invalidResponse := httptest.NewRecorder()
	adminHandler("admin", &fakeOperations{}).ServeHTTP(invalidResponse, authenticatedAdminRequest(http.MethodGet,
		"/admin/v1/audit-logs?request_id=bad%20request", ""))
	if invalidResponse.Code != http.StatusBadRequest || !strings.Contains(invalidResponse.Body.String(), `"code":"INVALID_REQUEST_ID"`) {
		t.Fatalf("expected invalid request id to receive 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}
}
