package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

var adSlotCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{2,79}$`)

type adCampaignRequest struct {
	ExpectedVersion int     `json:"expected_version"`
	Name            string  `json:"name"`
	Advertiser      string  `json:"advertiser"`
	SlotCode        string  `json:"slot_code"`
	Placement       string  `json:"placement"`
	PageScope       string  `json:"page_scope"`
	DeviceScope     string  `json:"device_scope"`
	Title           string  `json:"title"`
	Sponsor         string  `json:"sponsor"`
	ImageURL        string  `json:"image_url"`
	ImageAlt        string  `json:"image_alt"`
	TargetURL       *string `json:"target_url"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	Priority        int     `json:"priority"`
	StartsAt        string  `json:"starts_at"`
	EndsAt          string  `json:"ends_at"`
}

type adVersionRequest struct {
	ExpectedVersion int `json:"expected_version"`
}

type adCampaignResponse struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Advertiser   string  `json:"advertiser"`
	Version      int     `json:"version"`
	VersionState string  `json:"version_state"`
	Status       string  `json:"status"`
	SlotCode     string  `json:"slot_code"`
	Placement    string  `json:"placement"`
	PageScope    string  `json:"page_scope"`
	DeviceScope  string  `json:"device_scope"`
	Title        string  `json:"title"`
	Sponsor      string  `json:"sponsor"`
	ImageURL     string  `json:"image_url"`
	ImageAlt     string  `json:"image_alt"`
	TargetURL    *string `json:"target_url"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	Priority     int     `json:"priority"`
	StartsAt     string  `json:"starts_at"`
	EndsAt       string  `json:"ends_at"`
	PublishedAt  *string `json:"published_at"`
	PausedAt     *string `json:"paused_at"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type adCampaignListResponse struct {
	Items []adCampaignResponse `json:"items"`
}

type publicAdResponse struct {
	Slot        string  `json:"slot"`
	Placement   string  `json:"placement"`
	DeviceScope string  `json:"device_scope"`
	Title       string  `json:"title"`
	Sponsor     string  `json:"sponsor"`
	ImageURL    string  `json:"image_url"`
	ImageAlt    string  `json:"image_alt"`
	TargetURL   *string `json:"target_url"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	StartsAt    string  `json:"starts_at"`
	EndsAt      string  `json:"ends_at"`
}

func (s *Server) publicAd(w http.ResponseWriter, r *http.Request) {
	if !s.requireOperations(w, r) {
		return
	}
	repository, ok := s.operations.(operations.AdManagementRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "AD_SERVICE_UNAVAILABLE", "广告服务暂时不可用")
		return
	}
	slot := strings.TrimSpace(r.URL.Query().Get("slot"))
	placement := strings.TrimSpace(r.URL.Query().Get("placement"))
	if !adSlotCodePattern.MatchString(slot) || !oneOf(placement, "banner", "rail", "infeed", "native") {
		writeError(w, r, http.StatusBadRequest, "INVALID_AD_SLOT", "广告位不符合要求")
		return
	}
	item, err := repository.EffectiveAd(r.Context(), slot, placement, s.now().UTC())
	if errors.Is(err, operations.ErrNotFound) {
		w.Header().Set("Cache-Control", "public, max-age=30, must-revalidate")
		writeJSON(w, http.StatusOK, map[string]any{"creative": nil})
		return
	}
	if s.writeOperationsError(w, r, err) {
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30, must-revalidate")
	writeJSON(w, http.StatusOK, map[string]any{"creative": publicAdResponse{
		Slot: item.SlotCode, Placement: item.Placement, DeviceScope: item.DeviceScope, Title: item.Title, Sponsor: item.Sponsor,
		ImageURL: item.ImageURL, ImageAlt: item.ImageAlt, TargetURL: item.TargetURL,
		Width: item.Width, Height: item.Height, StartsAt: item.StartsAt.UTC().Format(time.RFC3339), EndsAt: item.EndsAt.UTC().Format(time.RFC3339),
	}})
}

func (s *Server) adminListAds(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	repository, ok := s.operations.(operations.AdManagementRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "AD_SERVICE_UNAVAILABLE", "广告服务暂时不可用")
		return
	}
	items, err := repository.ListAdCampaigns(r.Context(), 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := adCampaignListResponse{Items: make([]adCampaignResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, adCampaignItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminCreateAd(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	repository, ok := s.operations.(operations.AdManagementRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "AD_SERVICE_UNAVAILABLE", "广告服务暂时不可用")
		return
	}
	var request adCampaignRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	input, valid := validateAdCampaignRequest(w, r, request)
	if !valid {
		return
	}
	item, err := repository.CreateAdCampaign(r.Context(), input, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, adCampaignItem(item))
}

func (s *Server) adminUpdateAd(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	repository, ok := s.operations.(operations.AdManagementRepository)
	campaignID := chi.URLParam(r, "campaignID")
	if !ok || !uuidPattern.MatchString(campaignID) {
		writeError(w, r, http.StatusNotFound, "AD_NOT_FOUND", "未找到广告")
		return
	}
	var request adCampaignRequest
	if !s.decodeJSON(w, r, &request) || request.ExpectedVersion < 1 {
		if request.ExpectedVersion < 1 {
			writeError(w, r, http.StatusBadRequest, "INVALID_AD_VERSION", "请提供当前广告版本")
		}
		return
	}
	input, valid := validateAdCampaignRequest(w, r, request)
	if !valid {
		return
	}
	item, err := repository.UpdateAdCampaign(r.Context(), campaignID, request.ExpectedVersion, input, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, adCampaignItem(item))
}

func (s *Server) adminPublishAd(w http.ResponseWriter, r *http.Request) {
	s.adminAdVersionAction(w, r, true)
}

func (s *Server) adminPauseAd(w http.ResponseWriter, r *http.Request) {
	s.adminAdVersionAction(w, r, false)
}

func (s *Server) adminAdVersionAction(w http.ResponseWriter, r *http.Request, publish bool) {
	user, ok := s.requireAdminRole(w, r, "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	repository, ok := s.operations.(operations.AdManagementRepository)
	campaignID := chi.URLParam(r, "campaignID")
	if !ok || !uuidPattern.MatchString(campaignID) {
		writeError(w, r, http.StatusNotFound, "AD_NOT_FOUND", "未找到广告")
		return
	}
	var item operations.AdCampaign
	var err error
	if publish {
		var request adVersionRequest
		if !s.decodeJSON(w, r, &request) || request.ExpectedVersion < 1 {
			if request.ExpectedVersion < 1 {
				writeError(w, r, http.StatusBadRequest, "INVALID_AD_VERSION", "请提供当前广告版本")
			}
			return
		}
		item, err = repository.PublishAdCampaign(r.Context(), campaignID, request.ExpectedVersion, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	} else {
		if r.ContentLength > 0 {
			var request struct{}
			if !s.decodeJSON(w, r, &request) {
				return
			}
		}
		item, err = repository.PauseAdCampaign(r.Context(), campaignID, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	}
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, adCampaignItem(item))
}

func validateAdCampaignRequest(w http.ResponseWriter, r *http.Request, request adCampaignRequest) (operations.AdCampaignInput, bool) {
	request.Name, request.Advertiser = strings.TrimSpace(request.Name), strings.TrimSpace(request.Advertiser)
	request.SlotCode, request.Title, request.Sponsor = strings.TrimSpace(request.SlotCode), strings.TrimSpace(request.Title), strings.TrimSpace(request.Sponsor)
	request.ImageURL, request.ImageAlt = strings.TrimSpace(request.ImageURL), strings.TrimSpace(request.ImageAlt)
	startsAt, startsErr := time.Parse(time.RFC3339, request.StartsAt)
	endsAt, endsErr := time.Parse(time.RFC3339, request.EndsAt)
	expectedSize := map[string][2]int{"banner": {960, 90}, "rail": {160, 600}, "infeed": {640, 120}, "native": {640, 400}}[request.Placement]
	validText := len(request.Name) >= 1 && len(request.Name) <= 120 && len(request.Advertiser) >= 1 && len(request.Advertiser) <= 120 &&
		len(request.Title) >= 1 && len(request.Title) <= 120 && len(request.Sponsor) >= 1 && len(request.Sponsor) <= 120 && len(request.ImageAlt) <= 160
	if !validText || !adSlotCodePattern.MatchString(request.SlotCode) || !strings.HasPrefix(request.SlotCode, request.PageScope+".") ||
		!oneOf(request.Placement, "banner", "rail", "infeed", "native") || !oneOf(request.PageScope, "home", "work", "performer", "studio", "latest", "recent", "popular") ||
		!oneOf(request.DeviceScope, "all", "desktop", "mobile") || request.Width != expectedSize[0] || request.Height != expectedSize[1] ||
		request.Priority < -1000 || request.Priority > 1000 || startsErr != nil || endsErr != nil || !endsAt.After(startsAt) ||
		!validAdURL(request.ImageURL, true) || (request.TargetURL != nil && !validAdURL(strings.TrimSpace(*request.TargetURL), false)) {
		writeError(w, r, http.StatusBadRequest, "INVALID_AD", "广告内容、位置、尺寸、地址或时间不符合要求")
		return operations.AdCampaignInput{}, false
	}
	if request.TargetURL != nil {
		trimmed := strings.TrimSpace(*request.TargetURL)
		request.TargetURL = &trimmed
	}
	return operations.AdCampaignInput{
		Name: request.Name, Advertiser: request.Advertiser, SlotCode: request.SlotCode,
		Placement: request.Placement, PageScope: request.PageScope, DeviceScope: request.DeviceScope,
		Title: request.Title, Sponsor: request.Sponsor, ImageURL: request.ImageURL, ImageAlt: request.ImageAlt,
		TargetURL: request.TargetURL, Width: request.Width, Height: request.Height, Priority: request.Priority,
		StartsAt: startsAt.UTC(), EndsAt: endsAt.UTC(),
	}, true
}

func validAdURL(value string, allowLocal bool) bool {
	if allowLocal && strings.HasPrefix(value, "/ads/") && !strings.Contains(value, "..") && !strings.ContainsAny(value, "?#") {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func adCampaignItem(item operations.AdCampaign) adCampaignResponse {
	return adCampaignResponse{
		ID: item.ID, Name: item.Name, Advertiser: item.Advertiser, Version: item.Version,
		VersionState: item.VersionState, Status: item.Status, SlotCode: item.SlotCode,
		Placement: item.Placement, PageScope: item.PageScope, DeviceScope: item.DeviceScope,
		Title: item.Title, Sponsor: item.Sponsor, ImageURL: item.ImageURL, ImageAlt: item.ImageAlt,
		TargetURL: item.TargetURL, Width: item.Width, Height: item.Height, Priority: item.Priority,
		StartsAt: item.StartsAt.UTC().Format(time.RFC3339), EndsAt: item.EndsAt.UTC().Format(time.RFC3339),
		PublishedAt: formatRFC3339(item.PublishedAt), PausedAt: formatRFC3339(item.PausedAt),
		CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
