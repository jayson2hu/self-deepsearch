package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/account"
	"self-deepsearch/services/platform-api/internal/catalog"
	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/operations"
)

type deliveryCatalog struct{ *fakeCatalog }

func (repo deliveryCatalog) ListPerformers(context.Context, string, int) ([]catalog.PerformerSummary, error) {
	return []catalog.PerformerSummary{repo.performer.PerformerSummary}, nil
}
func (repo deliveryCatalog) LatestPerformers(ctx context.Context, limit int) ([]catalog.PerformerSummary, error) {
	return repo.ListPerformers(ctx, "", limit)
}
func (repo deliveryCatalog) StudioBySlug(context.Context, string) (catalog.StudioDetail, error) {
	return catalog.StudioDetail{StudioSummary: catalog.StudioSummary{ID: "studio", Name: "text retained"}, Works: repo.latest}, nil
}

type deliveryAccount struct {
	account.Repository
	work      catalog.WorkSummary
	performer catalog.PerformerSummary
	history   account.HistoryItem
}

type deliveryPolicyRepository struct {
	operations.Repository
	defaultOnly bool
	err         error
	calls       int
}

func (repository *deliveryPolicyRepository) MediaDeliveryDefaultOnly(context.Context, time.Time) (bool, error) {
	repository.calls++
	return repository.defaultOnly, repository.err
}

func (repo deliveryAccount) ListFavorites(context.Context, string, int) ([]catalog.WorkSummary, error) {
	return []catalog.WorkSummary{repo.work}, nil
}
func (repo deliveryAccount) ListHiddenWorks(context.Context, string, int) ([]catalog.WorkSummary, error) {
	return []catalog.WorkSummary{repo.work}, nil
}
func (repo deliveryAccount) ListFollowedWorks(context.Context, string, int) ([]catalog.WorkSummary, error) {
	return []catalog.WorkSummary{repo.work}, nil
}
func (repo deliveryAccount) ListFollows(context.Context, string, int) ([]catalog.PerformerSummary, error) {
	return []catalog.PerformerSummary{repo.performer}, nil
}
func (repo deliveryAccount) ListHistory(context.Context, string, int) ([]account.HistoryItem, error) {
	return []account.HistoryItem{repo.history}, nil
}

func TestMediaDeliveryModeCoversCatalogAndAccountHTTPWithoutMutatingRepository(t *testing.T) {
	url := "https://media.example.test/must-not-be-delivered.webp"
	image := &catalog.Image{URL: url, Width: 320, Height: 200, Renditions: []catalog.ImageRendition{{URL: url, Width: 640, Height: 400}}}
	work := catalog.WorkSummary{ID: "10000000-0000-4000-8000-000000000001", Slug: "test-001", Code: "TEST-001", Title: "text retained", ImageURL: &url, Image: image}
	performer := catalog.PerformerSummary{ID: "20000000-0000-4000-8000-000000000001", Slug: "test-person", Name: "text retained", ImageURL: &url, Image: image}
	repo := deliveryCatalog{&fakeCatalog{latest: []catalog.WorkSummary{work},
		detail:    catalog.WorkDetail{WorkSummary: work, Performers: []catalog.PerformerSummary{performer}, Images: []catalog.Image{*image}, RelatedWorks: []catalog.WorkSummary{work}},
		performer: catalog.PerformerDetail{PerformerSummary: performer, Works: []catalog.WorkSummary{work}, Images: []catalog.Image{*image}},
	}}
	accounts := deliveryAccount{work: work, performer: performer, history: account.HistoryItem{ID: work.ID, ContentType: "work", Title: "text retained", ImageURL: &url, ViewedAt: time.Date(2026, 9, 11, 1, 2, 3, 0, time.UTC)}}
	paths := []string{"/api/v1/site/home", "/api/v1/site/editorial", "/api/v1/works/latest", "/api/v1/works/recent", "/api/v1/works/popular", "/api/v1/search/works?q=TEST", "/api/v1/works/test-001", "/api/v1/performers", "/api/v1/performers/test-person", "/api/v1/studios/test-studio", "/api/v1/me/favorites", "/api/v1/me/follows", "/api/v1/me/followed-works", "/api/v1/me/hidden-works", "/api/v1/me/history"}
	for _, mode := range []string{"normal", "default_only", "invalid", "normal"} {
		handler := New(Options{Catalog: repo, Account: accounts, MediaDeliveryMode: mode, Identity: &fakeIdentityService{user: identity.User{ID: "user", Role: "user"}}})
		for _, path := range paths {
			t.Run(mode+path, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.AddCookie(&http.Cookie{Name: "sd_session", Value: "synthetic-session"})
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != 200 || !strings.Contains(response.Body.String(), "text retained") {
					t.Fatalf("text/route unavailable: %d %s", response.Code, response.Body.String())
				}
				if got := strings.Contains(response.Body.String(), url); got != (mode == "normal") {
					t.Fatalf("image mode %s mismatch: %s", mode, response.Body.String())
				}
				if mode != "normal" && (response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Media-Delivery-Mode") != "default_only") {
					t.Fatal("suppressed output is not marked/no-store")
				}
			})
		}
	}
	if *repo.latest[0].ImageURL != url || repo.detail.Images[0].Renditions[0].URL != url {
		t.Fatal("repository evidence changed")
	}
}

func TestMediaProjectionCopiesResponseAndKeepsNonMediaFields(t *testing.T) {
	url := "https://media.example.test/evidence.webp"
	item := homeItem{Title: "text", ImageURL: &url, Image: &imageResponse{URL: url}}
	values := []any{
		homeResponse{Sections: []homeSection{{Items: []homeItem{item}}}}, itemListResponse{Items: []homeItem{item}}, workSearchResponse{Items: []homeItem{item}},
		workDetailResponse{Images: []imageResponse{{URL: url}}, RelatedWorks: []homeItem{item}, Performers: []performerResponse{{ImageURL: &url}}},
		performerDetailResponse{ImageURL: &url, Images: []imageResponse{{URL: url}}, Works: []homeItem{item}}, studioDetailResponse{Works: []homeItem{item}},
		historyResponse{Items: []historyItemResponse{{Item: item, ViewedAt: "2026-09-11T01:02:03Z"}}},
	}
	for _, value := range values {
		before, _ := json.Marshal(value)
		projected, _ := json.Marshal(withoutDeliveryImages(value))
		after, _ := json.Marshal(value)
		if string(before) != string(after) || strings.Contains(string(projected), url) {
			t.Fatalf("projection mutated source or exposed media: %s", projected)
		}
	}
	evidence := feedbackResponse{EvidenceURL: &url, Message: "retain rights evidence"}
	if withoutDeliveryImages(evidence) != evidence {
		t.Fatal("feedback evidence was altered")
	}
}

func TestDynamicMediaDeliveryIsRequestScopedAndFailsClosed(t *testing.T) {
	url := "https://media.example.test/dynamic.webp"
	work := catalog.WorkSummary{ID: "10000000-0000-4000-8000-000000000001", Slug: "test-001", Code: "TEST-001", Title: "text retained", ImageURL: &url}
	catalogRepository := &fakeCatalog{latest: []catalog.WorkSummary{work}}

	for _, testCase := range []struct {
		name          string
		repository    *deliveryPolicyRepository
		wantImage     bool
		wantAvailable bool
	}{
		{name: "safe", repository: &deliveryPolicyRepository{}, wantImage: true, wantAvailable: true},
		{name: "stopped", repository: &deliveryPolicyRepository{defaultOnly: true}, wantImage: false, wantAvailable: true},
		{name: "repository-error", repository: &deliveryPolicyRepository{err: errors.New("database unavailable")}, wantImage: false, wantAvailable: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := New(Options{Catalog: catalogRepository, Operations: testCase.repository, MediaDeliveryDynamicMode: "enforce", MetricsToken: "synthetic-token"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/works/latest", nil))
			if response.Code != http.StatusOK || strings.Contains(response.Body.String(), url) != testCase.wantImage {
				t.Fatalf("unexpected dynamic response %d: %s", response.Code, response.Body.String())
			}
			wantMode := "default_only"
			if testCase.wantImage {
				wantMode = "normal"
			}
			if response.Header().Get("X-Media-Delivery-Mode") != wantMode || testCase.repository.calls != 1 {
				t.Fatalf("unexpected policy use: mode=%q calls=%d", response.Header().Get("X-Media-Delivery-Mode"), testCase.repository.calls)
			}
			request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			request.Header.Set("Authorization", "Bearer synthetic-token")
			metrics := httptest.NewRecorder()
			handler.ServeHTTP(metrics, request)
			available := "0"
			if testCase.wantAvailable {
				available = "1"
			}
			for _, expected := range []string{"self_deepsearch_media_delivery_dynamic_enabled 1", "self_deepsearch_media_delivery_policy_available " + available} {
				if !strings.Contains(metrics.Body.String(), expected) {
					t.Fatalf("metrics missing %q: %s", expected, metrics.Body.String())
				}
			}
		})
	}

	missing := httptest.NewRecorder()
	New(Options{Catalog: catalogRepository, MediaDeliveryDynamicMode: "enforce"}).ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/works/latest", nil))
	if strings.Contains(missing.Body.String(), url) || missing.Header().Get("X-Media-Delivery-Mode") != "default_only" {
		t.Fatalf("missing policy repository did not fail closed: %s", missing.Body.String())
	}

	manualRepository := &deliveryPolicyRepository{err: errors.New("must not be queried")}
	manual := httptest.NewRecorder()
	New(Options{Catalog: catalogRepository, Operations: manualRepository, MediaDeliveryMode: "default_only", MediaDeliveryDynamicMode: "enforce"}).ServeHTTP(manual, httptest.NewRequest(http.MethodGet, "/api/v1/works/latest", nil))
	if manualRepository.calls != 0 || strings.Contains(manual.Body.String(), url) {
		t.Fatalf("manual priority was not preserved: calls=%d body=%s", manualRepository.calls, manual.Body.String())
	}
}

func TestInternalMediaDeliveryPolicyContract(t *testing.T) {
	repository := &deliveryPolicyRepository{}
	handler := New(Options{Operations: repository, MediaDeliveryDynamicMode: "enforce"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, internalMediaDeliveryPolicyPath, nil))
	if response.Code != http.StatusOK || response.Body.String() != `{"mode":"normal"}` {
		t.Fatalf("unexpected policy response %d: %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("X-Media-Delivery-Mode") != "normal" ||
		response.Header().Get("Content-Length") != "17" {
		t.Fatalf("unexpected policy headers: %#v", response.Header())
	}

	repository.err = errors.New("database unavailable")
	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, internalMediaDeliveryPolicyPath, nil))
	if failed.Body.String() != `{"mode":"default_only"}` || failed.Header().Get("X-Media-Delivery-Mode") != "default_only" {
		t.Fatalf("policy dependency failure did not fail closed: %s", failed.Body.String())
	}

	for _, testCase := range []struct {
		name       string
		request    *http.Request
		wantStatus int
	}{
		{name: "method", request: httptest.NewRequest(http.MethodPost, internalMediaDeliveryPolicyPath, nil), wantStatus: http.StatusMethodNotAllowed},
		{name: "query", request: httptest.NewRequest(http.MethodGet, internalMediaDeliveryPolicyPath+"?unexpected=1", nil), wantStatus: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, testCase.request)
			if result.Code != testCase.wantStatus {
				t.Fatalf("expected %d, got %d: %s", testCase.wantStatus, result.Code, result.Body.String())
			}
			if result.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("internal policy errors must not be cached")
			}
		})
	}
}

func TestEdgeMediaDeliveryPolicyRequiresIndependentBearerAndFailsClosed(t *testing.T) {
	const token = "edge-policy-token-012345678901234567890123"
	repository := &deliveryPolicyRepository{}
	handler := New(Options{
		Operations: repository, MediaDeliveryDynamicMode: "enforce",
		MediaEdgePolicyMode: "enforce", MediaEdgePolicyToken: token,
	})

	for _, testCase := range []struct {
		name       string
		method     string
		target     string
		auth       []string
		wantStatus int
		wantBody   string
	}{
		{name: "missing", method: http.MethodGet, target: edgeMediaDeliveryPolicyPath, wantStatus: http.StatusNotFound},
		{name: "wrong", method: http.MethodGet, target: edgeMediaDeliveryPolicyPath, auth: []string{"Bearer wrong"}, wantStatus: http.StatusNotFound},
		{name: "duplicate", method: http.MethodGet, target: edgeMediaDeliveryPolicyPath, auth: []string{"Bearer " + token, "Bearer " + token}, wantStatus: http.StatusNotFound},
		{name: "method", method: http.MethodPost, target: edgeMediaDeliveryPolicyPath, auth: []string{"Bearer " + token}, wantStatus: http.StatusMethodNotAllowed},
		{name: "query", method: http.MethodGet, target: edgeMediaDeliveryPolicyPath + "?unexpected=1", auth: []string{"Bearer " + token}, wantStatus: http.StatusBadRequest},
		{name: "normal", method: http.MethodGet, target: edgeMediaDeliveryPolicyPath, auth: []string{"Bearer " + token}, wantStatus: http.StatusOK, wantBody: `{"mode":"normal"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.target, nil)
			for _, value := range testCase.auth {
				request.Header.Add("Authorization", value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || (testCase.wantBody != "" && response.Body.String() != testCase.wantBody) {
				t.Fatalf("unexpected edge policy response %d: %q", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("edge policy responses must not be cached by the API edge")
			}
		})
	}

	repository.err = errors.New("database unavailable")
	request := httptest.NewRequest(http.MethodGet, edgeMediaDeliveryPolicyPath, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, request)
	if failed.Code != http.StatusOK || failed.Body.String() != `{"mode":"default_only"}` ||
		failed.Header().Get("X-Media-Delivery-Mode") != "default_only" {
		t.Fatalf("edge policy dependency failure did not fail closed: %d %s", failed.Code, failed.Body.String())
	}
	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRequest.Header.Set("Authorization", "Bearer metrics-token-012345678901234567890")
	metrics := httptest.NewRecorder()
	New(Options{MediaEdgePolicyMode: "enforce", MediaEdgePolicyToken: token, MetricsToken: "metrics-token-012345678901234567890"}).ServeHTTP(metrics, metricsRequest)
	if !strings.Contains(metrics.Body.String(), "self_deepsearch_media_edge_policy_enabled 1") {
		t.Fatalf("edge policy enabled metric missing: %s", metrics.Body.String())
	}

	disabledRequest := httptest.NewRequest(http.MethodGet, edgeMediaDeliveryPolicyPath, nil)
	disabledRequest.Header.Set("Authorization", "Bearer "+token)
	disabled := httptest.NewRecorder()
	New(Options{MediaEdgePolicyMode: "off", MediaEdgePolicyToken: token}).ServeHTTP(disabled, disabledRequest)
	if disabled.Code != http.StatusNotFound || disabled.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("disabled edge policy must remain absent and no-store: %d %#v", disabled.Code, disabled.Header())
	}
}

func TestMediaModeDoesNotDisableReadinessAccountOrPrivateMetrics(t *testing.T) {
	handler := New(Options{MediaDeliveryMode: "default_only", MetricsToken: "synthetic-token", Database: fakeDatabase{schemaVersion: requiredDatabaseSchemaVersion}, Identity: &fakeIdentityService{user: identity.User{ID: "user", Role: "user"}}})
	for _, path := range []string{"/healthz", "/readyz", "/api/v1/me", "/metrics"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: "sd_session", Value: "session"})
		if path == "/metrics" {
			r.Header.Set("Authorization", "Bearer synthetic-token")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s unavailable: %d", path, w.Code)
		}
		if path == "/metrics" && !strings.Contains(w.Body.String(), "self_deepsearch_media_default_only 1") {
			t.Fatal("mode metric missing")
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code == 200 {
		t.Fatal("mode metric exposed publicly")
	}
}
