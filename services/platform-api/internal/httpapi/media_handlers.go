package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

var (
	sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	siteIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
)

type mediaObjectRequest struct {
	Rendition    string  `json:"rendition"`
	StorageScope string  `json:"storage_scope"`
	StorageKey   string  `json:"storage_key"`
	BackupPath   string  `json:"backup_path"`
	PublicURL    *string `json:"public_url"`
	SHA256       string  `json:"sha256"`
	MimeType     string  `json:"mime_type"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	ByteSize     int64   `json:"byte_size"`
}

type mediaManifestRequest struct {
	AssetID     string               `json:"asset_id"`
	EntityType  string               `json:"entity_type"`
	EntityID    string               `json:"entity_id"`
	AssetType   string               `json:"asset_type"`
	SourceType  string               `json:"source_type"`
	SourceURL   *string              `json:"source_url"`
	CheckedAt   string               `json:"checked_at"`
	Confidence  float64              `json:"confidence"`
	Purpose     string               `json:"purpose"`
	Position    int                  `json:"position"`
	IsPrimary   bool                 `json:"is_primary"`
	Reason      string               `json:"reason"`
	ToolVersion string               `json:"tool_version"`
	Objects     []mediaObjectRequest `json:"objects"`
}

type mediaManifestResponse struct {
	AssetID       string `json:"asset_id"`
	EntityMediaID string `json:"entity_media_id"`
	Version       int    `json:"version"`
	ObjectCount   int    `json:"object_count"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
}

func (s *Server) adminRegisterMediaManifest(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	var request mediaManifestRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	input, err := validateMediaManifest(request, s.mediaPublicBaseURL)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_MEDIA_MANIFEST", err.Error())
		return
	}
	item, err := s.operations.RegisterMediaManifest(r.Context(), input, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, mediaManifestDTO(item))
}

func validateMediaManifest(request mediaManifestRequest, publicBaseURL string) (operations.MediaManifestInput, error) {
	empty := operations.MediaManifestInput{}
	checkedAt, err := time.Parse(time.RFC3339, request.CheckedAt)
	request.Reason, request.SourceType = strings.TrimSpace(request.Reason), strings.TrimSpace(request.SourceType)
	if err != nil || !uuidPattern.MatchString(request.AssetID) || !uuidPattern.MatchString(request.EntityID) || !oneOf(request.EntityType, "work", "performer") ||
		!validMediaIdentity(request) || len(request.Reason) < 2 || len(request.Reason) > 1000 ||
		len(request.SourceType) < 1 || len(request.SourceType) > 100 || request.Confidence < 0 || request.Confidence > 1 ||
		request.Position < 0 || request.Position > 100 || request.ToolVersion != "media-python/1" || len(request.Objects) != 4 {
		return empty, errors.New("图片清单不符合要求")
	}
	if request.SourceURL != nil && !validHTTPURL(*request.SourceURL) {
		return empty, errors.New("图片来源链接不符合要求")
	}
	objects := make([]operations.MediaObjectInput, 0, len(request.Objects))
	seenRenditions := make(map[string]bool)
	for _, object := range request.Objects {
		object.StorageKey, object.BackupPath = strings.TrimSpace(object.StorageKey), strings.TrimSpace(object.BackupPath)
		object.PublicURL = cleanOptional(object.PublicURL)
		if seenRenditions[object.Rendition] || !oneOf(object.Rendition, "master", "w320", "w640", "w960") ||
			!validStorageScope(object.Rendition, object.StorageScope, object.StorageKey) ||
			!validGeneratedStorageKey(request, object) ||
			!validStorageKey(object.StorageKey) || !validBackupPath(object.BackupPath) || object.BackupPath != object.StorageKey ||
			!validRenditionURL(object.Rendition, object.PublicURL, object.StorageKey, publicBaseURL) || !sha256Pattern.MatchString(object.SHA256) ||
			object.MimeType != "image/webp" ||
			object.Width < 1 || object.Width > 20000 || object.Height < 1 || object.Height > 20000 ||
			object.ByteSize < 1 || object.ByteSize > 10*1024*1024 {
			return empty, errors.New("图片对象信息不符合要求")
		}
		seenRenditions[object.Rendition] = true
		objects = append(objects, operations.MediaObjectInput{
			Rendition: object.Rendition, StorageScope: object.StorageScope, StorageKey: object.StorageKey, BackupPath: object.BackupPath,
			PublicURL: object.PublicURL, SHA256: object.SHA256, MimeType: object.MimeType,
			Width: object.Width, Height: object.Height, ByteSize: object.ByteSize,
		})
	}
	for _, rendition := range []string{"master", "w320", "w640", "w960"} {
		if !seenRenditions[rendition] {
			return empty, errors.New("图片清单缺少必要尺寸")
		}
	}
	return operations.MediaManifestInput{
		AssetID: request.AssetID, EntityType: request.EntityType, EntityID: request.EntityID, AssetType: request.AssetType,
		SourceType: request.SourceType, SourceURL: cleanOptional(request.SourceURL), CheckedAt: checkedAt.UTC(),
		Confidence: request.Confidence, Purpose: request.Purpose, Position: request.Position,
		IsPrimary: request.IsPrimary, Reason: request.Reason, ToolVersion: request.ToolVersion, Objects: objects,
	}, nil
}

func mediaManifestDTO(item operations.MediaManifest) mediaManifestResponse {
	return mediaManifestResponse{AssetID: item.AssetID, EntityMediaID: item.EntityMediaID,
		Version: item.Version, ObjectCount: item.ObjectCount, Status: item.Status, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339)}
}

type primaryMediaResponse struct {
	AssetID       string `json:"asset_id"`
	EntityMediaID string `json:"entity_media_id"`
	Purpose       string `json:"purpose"`
}

type primaryMediaStateResponse struct {
	EntityType   string                `json:"entity_type"`
	EntityID     string                `json:"entity_id"`
	EntityStatus string                `json:"entity_status"`
	Primary      *primaryMediaResponse `json:"primary"`
}

type primaryMediaReplaceRequest struct {
	ExpectedAssetID string               `json:"expected_asset_id"`
	Manifest        mediaManifestRequest `json:"manifest"`
}

type primaryMediaReplaceResponse struct {
	Media                mediaManifestResponse `json:"media"`
	PreviousAssetID      string                `json:"previous_asset_id"`
	OldAssetRetired      bool                  `json:"old_asset_retired"`
	PrivateRetainedUntil *string               `json:"private_retained_until"`
}

func (s *Server) adminGetPrimaryMedia(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	entityType, entityID := r.URL.Query().Get("entity_type"), r.URL.Query().Get("entity_id")
	if !oneOf(entityType, "work", "performer") || !uuidPattern.MatchString(entityID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "资料标识不符合要求")
		return
	}
	state, err := s.operations.GetPrimaryMedia(r.Context(), entityType, entityID)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := primaryMediaStateResponse{EntityType: state.EntityType, EntityID: state.EntityID, EntityStatus: state.EntityStatus}
	if state.Primary != nil {
		response.Primary = &primaryMediaResponse{AssetID: state.Primary.AssetID, EntityMediaID: state.Primary.EntityMediaID, Purpose: state.Primary.Purpose}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminReplacePrimaryMedia(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	var request primaryMediaReplaceRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if !uuidPattern.MatchString(request.ExpectedAssetID) || !request.Manifest.IsPrimary || strings.EqualFold(request.ExpectedAssetID, request.Manifest.AssetID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_MEDIA_MANIFEST", "替换需要旧主图标识和不同的新主图资产")
		return
	}
	manifest, err := validateMediaManifest(request.Manifest, s.mediaPublicBaseURL)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_MEDIA_MANIFEST", err.Error())
		return
	}
	item, err := s.operations.ReplacePrimaryMedia(r.Context(), operations.ReplacePrimaryMediaInput{
		ExpectedAssetID: request.ExpectedAssetID, Manifest: manifest,
	}, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := primaryMediaReplaceResponse{Media: mediaManifestDTO(item.Media), PreviousAssetID: item.PreviousAssetID, OldAssetRetired: item.OldAssetRetired}
	if item.PrivateRetainedUntil != nil {
		deadline := item.PrivateRetainedUntil.UTC().Format(time.RFC3339)
		response.PrivateRetainedUntil = &deadline
	}
	writeJSON(w, http.StatusCreated, response)
}

func validGeneratedStorageKey(request mediaManifestRequest, object mediaObjectRequest) bool {
	if object.Rendition == "master" {
		return object.StorageKey == "media-master/"+strings.ToLower(request.AssetID)+"/v1/master.webp"
	}
	parts := strings.Split(object.StorageKey, "/")
	plural := "works"
	if request.EntityType == "performer" {
		plural = "performers"
	}
	return len(parts) == 7 && parts[0] == "media-public" && siteIDPattern.MatchString(parts[1]) &&
		parts[2] == plural && strings.EqualFold(parts[3], request.EntityID) && strings.EqualFold(parts[4], request.AssetID) &&
		parts[5] == "v1" && parts[6] == request.Purpose+"-"+object.Rendition+".webp"
}

func validStorageScope(rendition, scope, key string) bool {
	return (rendition == "master" && scope == "private" && strings.HasPrefix(key, "media-master/")) ||
		(rendition != "master" && scope == "public" && strings.HasPrefix(key, "media-public/"))
}

func validMediaIdentity(request mediaManifestRequest) bool {
	return (request.EntityType == "work" && request.AssetType == "work_image" && request.Purpose == "cover" && request.IsPrimary && request.Position == 0) ||
		(request.EntityType == "work" && request.AssetType == "work_image" && request.Purpose == "gallery" && !request.IsPrimary && request.Position >= 1 && request.Position <= 3) ||
		(request.EntityType == "performer" && request.AssetType == "performer_avatar" && request.Purpose == "avatar" && request.IsPrimary && request.Position == 0)
}

func validHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func validPublicMediaURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func validRenditionURL(rendition string, value *string, storageKey, publicBaseURL string) bool {
	if rendition == "master" {
		return value == nil
	}
	if value == nil || !validPublicMediaURL(*value) {
		return false
	}
	parsed, _ := url.Parse(*value)
	if !strings.HasSuffix(parsed.Path, "/"+storageKey) {
		return false
	}
	return publicBaseURL == "" || *value == publicBaseURL+"/"+storageKey
}

func validBackupPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, "\\\r\n\x00") || strings.HasPrefix(value, "/") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validStorageKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 1024 || strings.ContainsAny(value, "\\\r\n\x00") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." || segment == "." {
			return false
		}
	}
	return true
}
