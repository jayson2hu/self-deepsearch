package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

type inspectionOperations struct {
	fakeOperations
	health operations.SystemHealth
}

func (r *inspectionOperations) SystemHealth(context.Context, time.Time) (operations.SystemHealth, error) {
	return r.health, nil
}

func TestDailyInspectionHealthAndPrivateMetrics(t *testing.T) {
	fresh, stale := float64(600), float64(172801)
	for _, test := range []struct {
		name, status             string
		age                      *float64
		issues, defaults, failed int64
	}{
		{"never", "degraded", nil, 0, 0, 0}, {"healthy", "ok", &fresh, 0, 0, 0},
		{"stale", "degraded", &stale, 0, 0, 0}, {"publication", "degraded", &fresh, 2, 0, 0},
		{"default", "degraded", &fresh, 0, 1, 0}, {"interrupted", "degraded", &fresh, 0, 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &inspectionOperations{health: operations.SystemHealth{SchemaVersion: requiredDatabaseSchemaVersion,
				MediaInspectionAgeSeconds: test.age, MediaPublicationIssues: test.issues, DefaultImageFailures: test.defaults, FailedMediaInspections: test.failed}}
			response := httptest.NewRecorder()
			adminHandler("admin", repo).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet, "/admin/v1/system/health", ""))
			var body systemHealthResponse
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Status != test.status ||
				body.MediaPublicationIssues != test.issues || body.DefaultImageFailures != test.defaults || body.FailedMediaInspections != test.failed {
				t.Fatalf("health contract: %d %s", response.Code, response.Body)
			}
			for _, role := range []string{"editor", "user"} {
				denied := httptest.NewRecorder()
				adminHandler(role, repo).ServeHTTP(denied, authenticatedAdminRequest(http.MethodGet, "/admin/v1/system/health", ""))
				if denied.Code != 403 {
					t.Fatal("inspection disclosed to non-admin")
				}
			}
			handler := New(Options{Database: fakeDatabase{schemaVersion: requiredDatabaseSchemaVersion}, Operations: repo, MetricsToken: "test-inspection-metrics"})
			for _, authorized := range []bool{false, true} {
				request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
				if authorized {
					request.Header.Set("Authorization", "Bearer test-inspection-metrics")
				}
				metrics := httptest.NewRecorder()
				handler.ServeHTTP(metrics, request)
				if !authorized {
					if metrics.Code != 401 || strings.Contains(metrics.Body.String(), "media_publication_issues") {
						t.Fatal("metrics exposed")
					}
					continue
				}
				for _, name := range []string{"media_publication_issues", "default_image_failures", "media_inspection_failures_24h"} {
					if !strings.Contains(metrics.Body.String(), "self_deepsearch_"+name+" ") {
						t.Fatal("missing aggregate metric", name)
					}
				}
				if strings.Contains(metrics.Body.String(), "self_deepsearch_media_inspection_age_seconds ") != (test.age != nil) {
					t.Fatal("unknown inspection age misreported as zero")
				}
			}
		})
	}
}
