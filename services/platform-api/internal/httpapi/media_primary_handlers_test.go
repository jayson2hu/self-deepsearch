package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"self-deepsearch/services/platform-api/internal/operations"
)

const previousPrimaryID = "20000000-0000-4000-8000-000000000002"

func primaryReplaceBody() string {
	return `{"expected_asset_id":"` + previousPrimaryID + `","manifest":` + processorMediaManifestJSON + `}`
}

func TestPrimaryReadPermissionsNullableStateAndValidation(t *testing.T) {
	for _, hasPrimary := range []bool{false, true} {
		repository := &fakeOperations{primaryState: operations.PrimaryMediaState{EntityType: "work", EntityID: "10000000-0000-4000-8000-000000000001", EntityStatus: "draft"}}
		if hasPrimary {
			repository.primaryState.Primary = &operations.PrimaryMedia{AssetID: previousPrimaryID, EntityMediaID: "30000000-0000-4000-8000-000000000001", Purpose: "cover"}
		}
		response := httptest.NewRecorder()
		adminHandler("owner", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet, "/admin/v1/media/primary?entity_type=work&entity_id="+repository.primaryState.EntityID, ""))
		var body primaryMediaStateResponse
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || (body.Primary != nil) != hasPrimary || body.EntityStatus != "draft" {
			t.Fatalf("bad primary state: %d %s", response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "storage_key") {
			t.Fatal("primary read leaked or was cached")
		}
	}
	for _, test := range []struct {
		role, query string
		err         error
		status      int
	}{
		{"editor", "entity_type=work&entity_id=10000000-0000-4000-8000-000000000001", nil, 403},
		{"admin", "entity_type=studio&entity_id=10000000-0000-4000-8000-000000000001", nil, 400},
		{"admin", "entity_type=work&entity_id=bad", nil, 400},
		{"admin", "entity_type=work&entity_id=10000000-0000-4000-8000-000000000001", operations.ErrNotFound, 404},
	} {
		response := httptest.NewRecorder()
		adminHandler(test.role, &fakeOperations{operationErr: test.err}).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet, "/admin/v1/media/primary?"+test.query, ""))
		if response.Code != test.status {
			t.Fatalf("read boundary: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestPrimaryReplacementPreservesManifestAndReportsSharedRetention(t *testing.T) {
	for _, shared := range []bool{false, true} {
		repository := &fakeOperations{sharedMedia: shared}
		response := httptest.NewRecorder()
		adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/media/primary/replace", primaryReplaceBody()))
		var body primaryMediaReplaceResponse
		if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.PreviousAssetID != previousPrimaryID || body.OldAssetRetired == shared || (body.PrivateRetainedUntil == nil) != shared {
			t.Fatalf("bad replacement response: %d %s", response.Code, response.Body.String())
		}
		if repository.primaryInput.ExpectedAssetID != previousPrimaryID || len(repository.primaryInput.Manifest.Objects) != 4 || body.Media.AssetID != repository.primaryInput.Manifest.AssetID {
			t.Fatal("lost immutable manifest")
		}
	}
}

func TestPrimaryReplacementValidationAndFailureNeverConfirmSuccess(t *testing.T) {
	for _, test := range []struct {
		name, role, body string
		err              error
		status           int
	}{
		{"editor", "editor", primaryReplaceBody(), nil, 403},
		{"missing_expected", "admin", strings.Replace(primaryReplaceBody(), previousPrimaryID, "", 1), nil, 400},
		{"same_asset", "admin", strings.Replace(primaryReplaceBody(), previousPrimaryID, "20000000-0000-4000-8000-000000000001", 1), nil, 400},
		{"not_primary", "admin", strings.Replace(primaryReplaceBody(), `"is_primary":true`, `"is_primary":false`, 1), nil, 400},
		{"bad_object", "admin", strings.Replace(primaryReplaceBody(), `"byte_size":1200`, `"byte_size":0`, 1), nil, 400},
		{"stale", "admin", primaryReplaceBody(), operations.ErrConflict, 409},
		{"unavailable", "admin", primaryReplaceBody(), errors.New("private diagnostic"), 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeOperations{operationErr: test.err}
			response := httptest.NewRecorder()
			adminHandler(test.role, repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/media/primary/replace", test.body))
			if response.Code != test.status || strings.Contains(response.Body.String(), `"old_asset_retired"`) || strings.Contains(response.Body.String(), "private diagnostic") {
				t.Fatalf("bad failure: %d %s", response.Code, response.Body.String())
			}
			if test.err == nil && repository.primaryInput.ExpectedAssetID != "" {
				t.Fatal("invalid/forbidden input reached storage")
			}
		})
	}
}
