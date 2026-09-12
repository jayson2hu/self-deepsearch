package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

const internalMediaDeliveryPolicyPath = "/internal/v1/media-delivery-policy"
const edgeMediaDeliveryPolicyPath = "/edge/v1/media-delivery-policy"

// writeMediaJSON projects public/user-facing catalog DTOs only. Operator
// manifests, object evidence and the repository remain unchanged for recovery
// and rights deletion. This switch is not provider quota accounting.
func (s *Server) writeMediaJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	if !hasDeliveryImages(value) {
		writeJSON(w, status, value)
		return
	}
	defaultOnly, _ := s.effectiveMediaDelivery(r.Context())
	mode := "normal"
	if defaultOnly {
		mode, value = "default_only", withoutDeliveryImages(value)
		w.Header().Set("Cache-Control", "no-store")
	}
	w.Header().Set("X-Media-Delivery-Mode", mode)
	writeJSON(w, status, value)
}

// effectiveMediaDelivery returns the safest effective mode and whether all
// inputs needed to evaluate that mode were available. The manual switch wins;
// dynamic enforcement fails closed when its read-only repository is absent,
// slow or unavailable.
func (s *Server) effectiveMediaDelivery(ctx context.Context) (defaultOnly, available bool) {
	if s.mediaDefaultOnly {
		return true, true
	}
	if !s.mediaDeliveryDynamic {
		return false, true
	}
	repository, ok := s.operations.(operations.MediaDeliveryPolicyRepository)
	if !ok || isTypedNil(repository) {
		return true, false
	}
	lookupContext, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	defaultOnly, err := repository.MediaDeliveryDefaultOnly(lookupContext, s.now().UTC())
	if err != nil {
		return true, false
	}
	return defaultOnly, true
}

// internalMediaDeliveryPolicy is intentionally outside the public router and
// OpenAPI contract. The production edge blocks /internal/; display-web calls
// this exact path over the private service network on every document request.
func (s *Server) internalMediaDeliveryPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != internalMediaDeliveryPolicyPath {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请求方法不受支持")
			return
		}
		if r.URL.RawQuery != "" {
			writeError(w, r, http.StatusBadRequest, "INVALID_QUERY", "该内部策略接口不接受查询参数")
			return
		}
		s.writeMediaDeliveryPolicy(w, r)
	})
}

// edgeMediaDeliveryPolicy exposes the same read-only decision to the
// Cloudflare media gateway. It is disabled by default and deliberately uses a
// credential independent from account, metrics and cache-revalidation
// secrets. Authentication failures look like an absent route.
func (s *Server) edgeMediaDeliveryPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != edgeMediaDeliveryPolicyPath {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if !s.mediaEdgePolicy || len(s.mediaEdgePolicyToken) < 32 || !s.validMediaEdgeAuthorization(r) {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请求方法不受支持")
			return
		}
		if r.URL.RawQuery != "" {
			writeError(w, r, http.StatusBadRequest, "INVALID_QUERY", "该边缘策略接口不接受查询参数")
			return
		}
		s.writeMediaDeliveryPolicy(w, r)
	})
}

func (s *Server) validMediaEdgeAuthorization(r *http.Request) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return false
	}
	provided := strings.TrimPrefix(values[0], "Bearer ")
	if provided == "" || strings.TrimSpace(provided) != provided {
		return false
	}
	expectedHash := sha256.Sum256([]byte(s.mediaEdgePolicyToken))
	providedHash := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) == 1
}

func (s *Server) writeMediaDeliveryPolicy(w http.ResponseWriter, r *http.Request) {
	defaultOnly, _ := s.effectiveMediaDelivery(r.Context())
	mode := "normal"
	body := []byte("{\"mode\":\"normal\"}")
	if defaultOnly {
		mode = "default_only"
		body = []byte("{\"mode\":\"default_only\"}")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Media-Delivery-Mode", mode)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func hasDeliveryImages(value any) bool {
	switch value.(type) {
	case homeResponse, itemListResponse, workSearchResponse, workDetailResponse,
		performerDetailResponse, studioDetailResponse, historyResponse:
		return true
	default:
		return false
	}
}

func withoutDeliveryImages(value any) any {
	switch response := value.(type) {
	case homeResponse:
		response.Sections = slices.Clone(response.Sections)
		for index := range response.Sections {
			response.Sections[index].Items = withoutItemImages(response.Sections[index].Items)
		}
		return response
	case itemListResponse:
		response.Items = withoutItemImages(response.Items)
		return response
	case workSearchResponse:
		response.Items = withoutItemImages(response.Items)
		return response
	case workDetailResponse:
		response.Images = []imageResponse{}
		response.RelatedWorks = withoutItemImages(response.RelatedWorks)
		response.Performers = slices.Clone(response.Performers)
		for index := range response.Performers {
			response.Performers[index].ImageURL, response.Performers[index].Image = nil, nil
		}
		return response
	case performerDetailResponse:
		response.ImageURL, response.Images = nil, []imageResponse{}
		response.Works = withoutItemImages(response.Works)
		return response
	case studioDetailResponse:
		response.Works = withoutItemImages(response.Works)
		return response
	case historyResponse:
		response.Items = slices.Clone(response.Items)
		for index := range response.Items {
			response.Items[index].Item.ImageURL, response.Items[index].Item.Image = nil, nil
		}
		return response
	default:
		return value
	}
}

func withoutItemImages(items []homeItem) []homeItem {
	result := slices.Clone(items)
	for index := range result {
		result[index].ImageURL, result[index].Image = nil, nil
	}
	return result
}
