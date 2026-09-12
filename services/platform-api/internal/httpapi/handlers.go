package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/catalog"
	"self-deepsearch/services/platform-api/internal/logsafe"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var publicEntityPathPattern = regexp.MustCompile(`^/(?:works|performers|studios)/[a-z0-9]+(?:-[a-z0-9]+)*$`)

const requiredDatabaseSchemaVersion = 21

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
	Time    string `json:"time"`
}

type dependencyStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type readinessResponse struct {
	Status       string             `json:"status"`
	Service      string             `json:"service"`
	Version      string             `json:"version"`
	Dependencies []dependencyStatus `json:"dependencies"`
	Time         string             `json:"time"`
}

type homeItem struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Code     string         `json:"code,omitempty"`
	Title    string         `json:"title"`
	Subtitle string         `json:"subtitle"`
	Href     string         `json:"href"`
	ImageURL *string        `json:"image_url"`
	Image    *imageResponse `json:"image"`
}

type homeSection struct {
	Key   string     `json:"key"`
	Title string     `json:"title"`
	Items []homeItem `json:"items"`
}

type homeResponse struct {
	GeneratedAt string        `json:"generated_at"`
	Sections    []homeSection `json:"sections"`
}

type workSearchResponse struct {
	Query      string     `json:"query"`
	Items      []homeItem `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

type performerResponse struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Href     string         `json:"href"`
	ImageURL *string        `json:"image_url"`
	Image    *imageResponse `json:"image"`
}

type imageRenditionResponse struct {
	URL       string `json:"url"`
	Rendition string `json:"rendition"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MimeType  string `json:"mime_type"`
}

type imageResponse struct {
	URL        string                   `json:"url"`
	Rendition  string                   `json:"rendition"`
	Width      int                      `json:"width"`
	Height     int                      `json:"height"`
	MimeType   string                   `json:"mime_type"`
	Renditions []imageRenditionResponse `json:"renditions"`
}

type workDetailResponse struct {
	ID            string              `json:"id"`
	Code          string              `json:"code"`
	Title         string              `json:"title"`
	TitleOriginal *string             `json:"title_original"`
	ReleaseDate   *string             `json:"release_date"`
	StudioName    string              `json:"studio_name"`
	Summary       *string             `json:"summary"`
	Performers    []performerResponse `json:"performers"`
	Images        []imageResponse     `json:"images"`
	RelatedWorks  []homeItem          `json:"related_works"`
}

type sitemapEntryResponse struct {
	EntityType string `json:"entity_type"`
	Slug       string `json:"slug"`
	UpdatedAt  string `json:"updated_at"`
}

type sitemapResponse struct {
	Items []sitemapEntryResponse `json:"items"`
}

type redirectTargetResponse struct {
	TargetPath string `json:"target_path"`
	StatusCode int    `json:"status_code"`
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.writeMediaJSON(w, r, http.StatusOK, healthResponse{
		Status: "ok", Service: s.service, Version: s.version,
		Time: s.now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	response := readinessResponse{
		Status: "ready", Service: s.service, Version: s.version,
		Time: s.now().UTC().Format(time.RFC3339),
	}
	status := http.StatusOK

	if s.database == nil {
		response.Status = "not_ready"
		status = http.StatusServiceUnavailable
		response.Dependencies = append(response.Dependencies, dependencyStatus{
			Name: "database", Status: "not_configured", Detail: "DATABASE_URL is not configured",
		})
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.database.Ping(ctx); err != nil {
			response.Status = "not_ready"
			status = http.StatusServiceUnavailable
			response.Dependencies = append(response.Dependencies, dependencyStatus{
				Name: "database", Status: "unavailable", Detail: "database ping failed",
			})
		} else {
			schemaVersion, err := s.database.SchemaVersion(ctx)
			switch {
			case err != nil:
				response.Status = "not_ready"
				status = http.StatusServiceUnavailable
				response.Dependencies = append(response.Dependencies, dependencyStatus{
					Name: "database", Status: "unavailable", Detail: "database schema version query failed",
				})
			case schemaVersion != requiredDatabaseSchemaVersion:
				response.Status = "not_ready"
				status = http.StatusServiceUnavailable
				response.Dependencies = append(response.Dependencies, dependencyStatus{
					Name: "database", Status: "schema_mismatch",
					Detail: fmt.Sprintf("expected schema version %d, got %d", requiredDatabaseSchemaVersion, schemaVersion),
				})
			default:
				response.Dependencies = append(response.Dependencies, dependencyStatus{Name: "database", Status: "ok"})
			}
		}
	}
	s.writeMediaJSON(w, r, status, response)
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rule, err := s.catalog.DiscoveryMixRule(ctx)
	if err != nil {
		s.writeCatalogError(w, r, "query_home_discovery_rule", err)
		return
	}

	latestLimit, performerLimit := discoverySourceLimits(rule)
	latest, err := s.catalog.LatestWorks(ctx, "", latestLimit)
	if err != nil {
		s.writeCatalogError(w, r, "query_home_latest", err)
		return
	}
	editorial, err := s.catalog.EditorialWorks(ctx, 4)
	if err != nil {
		s.writeCatalogError(w, r, "query_home_editorial", err)
		return
	}
	performers, err := s.catalog.LatestPerformers(ctx, performerLimit)
	if err != nil {
		s.writeCatalogError(w, r, "query_home_performers", err)
		return
	}
	trending, err := s.catalog.TrendingWorks(ctx, 7*24*time.Hour, "", 4)
	if err != nil {
		s.writeCatalogError(w, r, "query_home_trending", err)
		return
	}
	month := 30 * 24 * time.Hour
	mostViewed, err := s.catalog.MostViewedWorks(ctx, &month, "", 4)
	if err != nil {
		s.writeCatalogError(w, r, "query_home_most_viewed", err)
		return
	}

	s.writeMediaJSON(w, r, http.StatusOK, homeResponse{
		GeneratedAt: s.now().UTC().Format(time.RFC3339),
		Sections: []homeSection{
			{Key: "discovery", Title: "发现", Items: mixDiscoveryItems(latest, performers, rule)},
			{Key: "latest", Title: "最新发行", Items: workItems(latest)},
			{Key: "editorial", Title: "编辑推荐", Items: workItems(editorial)},
			{Key: "performers", Title: "人物资料", Items: performerItems(performers)},
			{Key: "trending", Title: "近期热门", Items: workItems(trending)},
			{Key: "most_viewed", Title: "浏览最多", Items: workItems(mostViewed)},
		},
	})
}

func discoverySourceLimits(rule catalog.DiscoveryMixRule) (int, int) {
	rule = normalizeDiscoveryMixRule(rule)
	cycle := rule.WorkSlots + rule.PerformerSlots
	// The persisted rule is validated at the write boundary, but keep this
	// read path defensive so a malformed fixture or a hand-edited row cannot
	// produce a zero-progress loop or an invalid division.
	if cycle <= 0 || rule.RepeatWindow <= 0 {
		return 8, 4
	}
	cycles := (rule.RepeatWindow-1)/cycle + 1
	return max(8, cycles*rule.WorkSlots), max(4, cycles*rule.PerformerSlots)
}

func mixDiscoveryItems(works []catalog.WorkSummary, performers []catalog.PerformerSummary, rule catalog.DiscoveryMixRule) []homeItem {
	rule = normalizeDiscoveryMixRule(rule)
	workValues, performerValues := workItems(works), performerItems(performers)
	result := make([]homeItem, 0, rule.RepeatWindow)
	workIndex, performerIndex := 0, 0
	for len(result) < rule.RepeatWindow && (workIndex < len(workValues) || performerIndex < len(performerValues)) {
		for count := 0; count < rule.WorkSlots && workIndex < len(workValues) && len(result) < rule.RepeatWindow; count++ {
			result = append(result, workValues[workIndex])
			workIndex++
		}
		for count := 0; count < rule.PerformerSlots && performerIndex < len(performerValues) && len(result) < rule.RepeatWindow; count++ {
			result = append(result, performerValues[performerIndex])
			performerIndex++
		}
	}
	return result
}

func normalizeDiscoveryMixRule(rule catalog.DiscoveryMixRule) catalog.DiscoveryMixRule {
	if rule.WorkSlots <= 0 {
		rule.WorkSlots = 4
	}
	if rule.PerformerSlots <= 0 {
		rule.PerformerSlots = 1
	}
	if rule.RepeatWindow <= 0 {
		rule.RepeatWindow = 10
	}
	return rule
}

func (s *Server) editorialWorks(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	items, err := s.catalog.EditorialWorks(ctx, 100)
	if err != nil {
		s.writeCatalogError(w, r, "query_editorial_works", err)
		return
	}
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: workItems(items)})
}

func (s *Server) popularWorks(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	rangeValue := strings.TrimSpace(r.URL.Query().Get("range"))
	if rangeValue == "" {
		rangeValue = "30d"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	scope := "works.popular." + rangeValue
	afterID, ok := readPageCursor(w, r, scope)
	if !ok {
		return
	}
	var (
		items []catalog.WorkSummary
		err   error
	)
	switch rangeValue {
	case "7d":
		items, err = s.catalog.TrendingWorks(ctx, 7*24*time.Hour, afterID, publicPageSize+1)
	case "30d":
		window := 30 * 24 * time.Hour
		items, err = s.catalog.MostViewedWorks(ctx, &window, afterID, publicPageSize+1)
	case "all":
		items, err = s.catalog.MostViewedWorks(ctx, nil, afterID, publicPageSize+1)
	default:
		writeError(w, r, http.StatusBadRequest, "INVALID_POPULAR_RANGE", "排行时间范围不符合要求")
		return
	}
	if err != nil {
		s.writeCatalogError(w, r, "query_popular_works", err)
		return
	}
	page, nextCursor := pageValues(items, scope)
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: workItems(page), NextCursor: nextCursor})
}

func (s *Server) latestWorks(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	afterID, ok := readPageCursor(w, r, "works.latest")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	items, err := s.catalog.LatestWorks(ctx, afterID, publicPageSize+1)
	if err != nil {
		s.writeCatalogError(w, r, "query_latest_works", err)
		return
	}
	page, nextCursor := pageValues(items, "works.latest")
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: workItems(page), NextCursor: nextCursor})
}

func (s *Server) recentlyAddedWorks(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	afterID, ok := readPageCursor(w, r, "works.recent")
	if !ok {
		return
	}
	items, err := s.catalog.RecentlyAddedWorks(ctx, afterID, publicPageSize+1)
	if err != nil {
		s.writeCatalogError(w, r, "query_recently_added_works", err)
		return
	}
	page, nextCursor := pageValues(items, "works.recent")
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: workItems(page), NextCursor: nextCursor})
}

func (s *Server) searchWorks(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	rawQuery := normalizeSearchQuery(r.URL.Query().Get("q"))
	compactQuery := compactCode(rawQuery)
	if len([]rune(rawQuery)) < 2 || len([]rune(rawQuery)) > 100 || len(compactQuery) < 2 {
		writeError(w, r, http.StatusBadRequest, "INVALID_QUERY", "请输入 2 至 100 个字符的番号、标题、人物或厂牌")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	scope := searchCursorScope(rawQuery)
	afterID, ok := readPageCursor(w, r, scope)
	if !ok {
		return
	}
	works, err := s.catalog.SearchWorks(ctx, rawQuery, compactQuery, afterID, publicPageSize+1)
	if err != nil {
		s.writeCatalogError(w, r, "search_works", err)
		return
	}
	page, nextCursor := pageValues(works, scope)
	if afterID == "" {
		s.recordSearchOutcome(r, len(page) == 0)
	}
	s.writeMediaJSON(w, r, http.StatusOK, workSearchResponse{Query: rawQuery, Items: workItems(page), NextCursor: nextCursor})
}

func (s *Server) recordSearchOutcome(r *http.Request, zeroResult bool) {
	if s.catalog == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
	defer cancel()
	if err := s.catalog.RecordSearchOutcome(ctx, zeroResult); err != nil {
		s.logger.WarnContext(r.Context(), "search_metrics_failed", "request_id", requestIDFromContext(r.Context()), "error_class", logsafe.ErrorClass(err))
	}
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	entityType := strings.TrimSpace(r.URL.Query().Get("entity_type"))
	if entityType == "" {
		entityType = "all"
	}
	if !oneOf(entityType, "all", "work", "performer", "studio") {
		writeError(w, r, http.StatusBadRequest, "INVALID_ENTITY_TYPE", "sitemap 实体类型不符合要求")
		return
	}
	entries, err := s.catalog.SitemapEntries(ctx, entityType, 50000)
	if err != nil {
		s.writeCatalogError(w, r, "query_sitemap", err)
		return
	}
	response := sitemapResponse{Items: make([]sitemapEntryResponse, 0, len(entries))}
	for _, entry := range entries {
		response.Items = append(response.Items, sitemapEntryResponse{
			EntityType: entry.EntityType, Slug: entry.Slug, UpdatedAt: entry.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	s.writeMediaJSON(w, r, http.StatusOK, response)
}

func (s *Server) publicRedirect(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	sourcePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if len(sourcePath) > 300 || !publicEntityPathPattern.MatchString(sourcePath) {
		writeError(w, r, http.StatusBadRequest, "INVALID_REDIRECT_PATH", "重定向路径不符合要求")
		return
	}
	target, err := s.catalog.RedirectTarget(r.Context(), sourcePath)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "REDIRECT_NOT_FOUND", "未找到重定向")
		return
	}
	if err != nil {
		s.writeCatalogError(w, r, "query_public_redirect", err)
		return
	}
	if target.StatusCode != http.StatusMovedPermanently || !publicEntityPathPattern.MatchString(target.TargetPath) {
		s.writeCatalogError(w, r, "reject_unsafe_public_redirect", errors.New("unsafe public redirect record"))
		return
	}
	s.writeMediaJSON(w, r, http.StatusOK, redirectTargetResponse{TargetPath: target.TargetPath, StatusCode: target.StatusCode})
}

func (s *Server) workDetail(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	slug := chi.URLParam(r, "slug")
	if !slugPattern.MatchString(slug) {
		writeError(w, r, http.StatusNotFound, "WORK_NOT_FOUND", "未找到该作品资料")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	detail, err := s.catalog.WorkBySlug(ctx, slug)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "WORK_NOT_FOUND", "未找到该作品资料")
		return
	}
	if err != nil {
		s.writeCatalogError(w, r, "query_work_detail", err)
		return
	}
	response := workDetailResponse{
		ID: detail.ID, Code: detail.Code, Title: detail.Title, TitleOriginal: detail.TitleOriginal,
		ReleaseDate: formatDate(detail.ReleaseDate), StudioName: detail.StudioName, Summary: detail.Summary,
		Performers:   make([]performerResponse, 0, len(detail.Performers)),
		Images:       make([]imageResponse, 0, len(detail.Images)),
		RelatedWorks: workItems(detail.RelatedWorks),
	}
	for _, performer := range detail.Performers {
		response.Performers = append(response.Performers, performerResponse{
			ID: performer.ID, Name: performer.Name, Href: "/performers/" + performer.Slug,
			ImageURL: performer.ImageURL, Image: imageResponseFromCatalog(performer.Image),
		})
	}
	for _, item := range detail.Images {
		response.Images = append(response.Images, *imageResponseFromCatalog(&item))
	}
	s.writeMediaJSON(w, r, http.StatusOK, response)
}

func (s *Server) writeCatalogError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	s.logger.ErrorContext(r.Context(), operation,
		"request_id", requestIDFromContext(r.Context()), "error_class", logsafe.ErrorClass(err))
	writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
}

func workItems(works []catalog.WorkSummary) []homeItem {
	items := make([]homeItem, 0, len(works))
	for _, work := range works {
		subtitle := formatDateValue(work.ReleaseDate)
		if work.StudioName != "" {
			if subtitle != "" {
				subtitle += " · "
			}
			subtitle += work.StudioName
		}
		items = append(items, homeItem{
			ID: work.ID, Type: "work", Code: work.Code, Title: work.Title,
			Subtitle: subtitle, Href: "/works/" + work.Slug,
			ImageURL: work.ImageURL, Image: imageResponseFromCatalog(work.Image),
		})
	}
	return items
}

func performerItems(performers []catalog.PerformerSummary) []homeItem {
	items := make([]homeItem, 0, len(performers))
	for _, performer := range performers {
		items = append(items, homeItem{
			ID: performer.ID, Type: "performer", Title: performer.Name,
			Subtitle: "人物资料", Href: "/performers/" + performer.Slug,
			ImageURL: performer.ImageURL, Image: imageResponseFromCatalog(performer.Image),
		})
	}
	return items
}

func imageResponseFromCatalog(image *catalog.Image) *imageResponse {
	if image == nil {
		return nil
	}
	renditions := image.Renditions
	if len(renditions) == 0 && image.URL != "" {
		renditions = []catalog.ImageRendition{{
			URL: image.URL, Rendition: image.Rendition, Width: image.Width,
			Height: image.Height, MimeType: image.MimeType,
		}}
	}
	response := &imageResponse{
		URL: image.URL, Rendition: image.Rendition, Width: image.Width,
		Height: image.Height, MimeType: image.MimeType,
		Renditions: make([]imageRenditionResponse, 0, len(renditions)),
	}
	for _, rendition := range renditions {
		response.Renditions = append(response.Renditions, imageRenditionResponse{
			URL: rendition.URL, Rendition: rendition.Rendition,
			Width: rendition.Width, Height: rendition.Height, MimeType: rendition.MimeType,
		})
	}
	return response
}

func compactCode(value string) string {
	var result strings.Builder
	for _, character := range strings.ToUpper(normalizeSearchQuery(value)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			result.WriteRune(character)
		}
	}
	return result.String()
}

func normalizeSearchQuery(value string) string {
	var folded strings.Builder
	for _, character := range value {
		switch {
		case character == '\u3000':
			folded.WriteByte(' ')
		case character >= '\uff01' && character <= '\uff5e':
			folded.WriteRune(character - 0xfee0)
		default:
			folded.WriteRune(character)
		}
	}
	return strings.Join(strings.Fields(folded.String()), " ")
}

func formatDate(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.DateOnly)
	return &formatted
}

func formatDateValue(value *time.Time) string {
	formatted := formatDate(value)
	if formatted == nil {
		return ""
	}
	return *formatted
}
