package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

type fakeAdManagement struct {
	*fakeOperations
	item         operations.AdCampaign
	effectiveErr error
	created      operations.AdCampaignInput
}

func (repository *fakeAdManagement) ListAdCampaigns(context.Context, int) ([]operations.AdCampaign, error) {
	return []operations.AdCampaign{repository.item}, repository.operationErr
}

func (repository *fakeAdManagement) CreateAdCampaign(_ context.Context, input operations.AdCampaignInput, _, _ string, _ time.Time) (operations.AdCampaign, error) {
	repository.created = input
	return repository.item, repository.operationErr
}

func (repository *fakeAdManagement) UpdateAdCampaign(context.Context, string, int, operations.AdCampaignInput, string, string, time.Time) (operations.AdCampaign, error) {
	return repository.item, repository.operationErr
}

func (repository *fakeAdManagement) PublishAdCampaign(context.Context, string, int, string, string, time.Time) (operations.AdCampaign, error) {
	return repository.item, repository.operationErr
}

func (repository *fakeAdManagement) PauseAdCampaign(context.Context, string, string, string, time.Time) (operations.AdCampaign, error) {
	return repository.item, repository.operationErr
}

func (repository *fakeAdManagement) EffectiveAd(context.Context, string, string, time.Time) (operations.AdCampaign, error) {
	return repository.item, repository.effectiveErr
}

func testAdCampaign() operations.AdCampaign {
	start := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	return operations.AdCampaign{ID: "10000000-0000-4000-8000-000000000001", Name: "首页广告", Advertiser: "示例广告主",
		Version: 1, VersionState: "published", Status: "live", SlotCode: "home.after_latest", Placement: "banner",
		PageScope: "home", DeviceScope: "mobile", Title: "合规素材", Sponsor: "示例广告主", ImageURL: "/ads/example.webp",
		ImageAlt: "", Width: 960, Height: 90, StartsAt: start, EndsAt: end, CreatedAt: start, UpdatedAt: start}
}

func TestPublicAdReturnsReviewedCreativeAndNoFill(t *testing.T) {
	repository := &fakeAdManagement{fakeOperations: &fakeOperations{}, item: testAdCampaign()}
	response := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/site/ads?slot=home.after_latest&placement=banner", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"device_scope":"mobile"`) ||
		response.Header().Get("Cache-Control") != "public, max-age=30, must-revalidate" {
		t.Fatalf("unexpected public ad response %d %s %#v", response.Code, response.Body.String(), response.Header())
	}

	repository.effectiveErr = operations.ErrNotFound
	response = httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/site/ads?slot=home.after_latest&placement=banner", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"creative":null`) {
		t.Fatalf("unexpected no-fill response %d %s", response.Code, response.Body.String())
	}
}

func TestAdminAdCreateValidatesPlacementDimensionsAndRole(t *testing.T) {
	repository := &fakeAdManagement{fakeOperations: &fakeOperations{}, item: testAdCampaign()}
	valid := `{"name":"首页广告","advertiser":"示例广告主","slot_code":"home.after_latest","placement":"banner","page_scope":"home","device_scope":"all","title":"合规素材","sponsor":"示例广告主","image_url":"/ads/example.webp","image_alt":"","target_url":null,"width":960,"height":90,"priority":1,"starts_at":"2026-09-17T00:00:00Z","ends_at":"2026-09-18T00:00:00Z"}`
	response := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/ads", valid))
	if response.Code != http.StatusCreated || repository.created.SlotCode != "home.after_latest" {
		t.Fatalf("unexpected create response %d %s", response.Code, response.Body.String())
	}

	native := `{"name":"首页推荐原生广告","advertiser":"示例广告主","slot_code":"home.recommendation_card","placement":"native","page_scope":"home","device_scope":"all","title":"合规原生素材","sponsor":"示例广告主","image_url":"/ads/example.webp","image_alt":"","target_url":null,"width":640,"height":400,"priority":1,"starts_at":"2026-09-17T00:00:00Z","ends_at":"2026-09-18T00:00:00Z"}`
	response = httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/ads", native))
	if response.Code != http.StatusCreated || repository.created.Placement != "native" || repository.created.Width != 640 || repository.created.Height != 400 {
		t.Fatalf("unexpected native create response %d %s", response.Code, response.Body.String())
	}

	invalid := strings.Replace(valid, `"width":960`, `"width":959`, 1)
	response = httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/ads", invalid))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid dimensions returned %d: %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	adminHandler("editor", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/ads", valid))
	if response.Code != http.StatusForbidden {
		t.Fatalf("editor create returned %d: %s", response.Code, response.Body.String())
	}
}
