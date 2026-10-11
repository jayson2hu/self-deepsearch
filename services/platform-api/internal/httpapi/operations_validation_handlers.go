package httpapi

import (
	"net/url"
	"regexp"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

var adminSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validEntityType(value string) bool {
	return oneOf(value, "work", "performer", "studio")
}

func validUniqueUUIDs(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !uuidPattern.MatchString(value) {
			return false
		}
		normalized := strings.ToLower(value)
		if _, exists := seen[normalized]; exists {
			return false
		}
		seen[normalized] = struct{}{}
	}
	return true
}

func normalizePerformerAliases(values []string) ([]string, bool) {
	if len(values) > 30 {
		return nil, false
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		if length := len([]rune(value)); length < 1 || length > 300 {
			return nil, false
		}
		normalized := strings.ToLower(value)
		if _, exists := seen[normalized]; exists {
			return nil, false
		}
		seen[normalized] = struct{}{}
		result = append(result, value)
	}
	return result, true
}

func normalizeSourceEvidence(values []sourceEvidenceRequest, now time.Time) ([]operations.SourceEvidenceInput, bool) {
	if len(values) < 1 || len(values) > 5 {
		return nil, false
	}
	result := make([]operations.SourceEvidenceInput, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		sourceType := strings.ToLower(strings.TrimSpace(value.SourceType))
		if !oneOf(sourceType, "web_page", "search_result", "official", "other") {
			return nil, false
		}
		sourceURL := cleanOptional(value.SourceURL)
		if sourceURL != nil {
			parsed, err := url.ParseRequestURI(*sourceURL)
			if err != nil || !oneOf(strings.ToLower(parsed.Scheme), "http", "https") || parsed.Host == "" || len([]rune(*sourceURL)) > 2000 {
				return nil, false
			}
		}
		sourceTitle := cleanOptional(value.SourceTitle)
		if sourceTitle != nil && len([]rune(*sourceTitle)) > 300 {
			return nil, false
		}
		checkedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(value.CheckedAt))
		if err != nil || checkedAt.After(now.Add(5*time.Minute)) {
			return nil, false
		}
		checkedAt = checkedAt.UTC().Truncate(time.Second)
		key := sourceType + "\x00" + strings.ToLower(optionalStringValue(sourceURL)) + "\x00" + strings.ToLower(optionalStringValue(sourceTitle))
		if _, exists := seen[key]; exists {
			return nil, false
		}
		seen[key] = struct{}{}
		result = append(result, operations.SourceEvidenceInput{
			SourceType: sourceType, SourceURL: sourceURL, SourceTitle: sourceTitle, CheckedAt: checkedAt,
		})
	}
	return result, true
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizePerformerAliasRevisionPayload(payload map[string]any) bool {
	value, exists := payload["aliases"]
	if !exists {
		return true
	}
	aliases, ok := performerAliasesFromPayload(value)
	if !ok {
		return false
	}
	normalized := make([]any, len(aliases))
	for index, alias := range aliases {
		normalized[index] = alias
	}
	payload["aliases"] = normalized
	return true
}

func performerAliasesFromPayload(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		values = append(values, text)
	}
	return normalizePerformerAliases(values)
}

func validOptionalRange(value *int, minimum, maximum int) bool {
	return value == nil || (*value >= minimum && *value <= maximum)
}

func validRevisionPayload(entityType string, payload map[string]any) bool {
	allowed := map[string]map[string]bool{
		"work":      {"canonical_code": true, "compact_code": true, "title": true, "title_original": true, "release_date": true, "studio_id": true, "summary": true, "performer_ids": true, "content_classification": true, "seo_quality_status": true},
		"performer": {"display_name": true, "name_original": true, "romanized_name": true, "aliases": true, "adult_status": true, "activity_status": true, "agency": true, "birth_year": true, "height_cm": true, "measurements": true, "debut_year": true, "content_classification": true, "seo_quality_status": true},
		"studio":    {"name": true, "normalized_name": true, "content_classification": true, "seo_quality_status": true},
	}[entityType]
	if allowed == nil {
		return false
	}
	for key, value := range payload {
		if !allowed[key] {
			return false
		}
		if value == nil {
			// A null patch explicitly clears an optional field. Required fields
			// remain non-null so a revision cannot make a published record
			// unusable. The database apply path already understands JSON null.
			return revisionFieldAllowsNull(entityType, key)
		}
		switch value.(type) {
		case string, float64:
		case []any:
			if !((entityType == "work" && key == "performer_ids") || (entityType == "performer" && key == "aliases")) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func revisionFieldAllowsNull(entityType, field string) bool {
	switch entityType {
	case "work":
		return oneOf(field, "title_original", "release_date", "studio_id", "summary", "performer_ids")
	case "performer":
		return oneOf(field, "name_original", "romanized_name", "agency", "birth_year", "height_cm", "measurements", "debut_year")
	default:
		return false
	}
}

func validRevisionPayloadValues(entityType string, payload map[string]any) bool {
	if !validSEOPolicyPayload(payload) {
		return false
	}
	switch entityType {
	case "work":
		_, hasCode := payload["canonical_code"]
		_, hasCompactCode := payload["compact_code"]
		if hasCompactCode && !hasCode {
			return false
		}
		return validRequiredString(payload, "canonical_code", 100) && validRequiredString(payload, "compact_code", 100) &&
			validRequiredString(payload, "title", 500) && validOptionalString(payload, "title_original", 500) &&
			validOptionalString(payload, "summary", 5000) && validOptionalUUID(payload, "studio_id") &&
			validOptionalDate(payload, "release_date") && validPerformerIDPayload(payload)
	case "performer":
		return validRequiredString(payload, "display_name", 200) && validOptionalString(payload, "name_original", 200) &&
			validOptionalString(payload, "romanized_name", 200) && validOptionalString(payload, "agency", 200) &&
			validOptionalString(payload, "measurements", 200) && validOptionalEnum(payload, "adult_status", "unverified", "verified", "restricted") &&
			validOptionalEnum(payload, "activity_status", "active", "retired", "unknown") &&
			validOptionalNumber(payload, "birth_year", 1900, 2200) && validOptionalNumber(payload, "height_cm", 100, 250) &&
			validOptionalNumber(payload, "debut_year", 1900, 2200) && validPerformerAliasPayload(payload)
	case "studio":
		_, hasName := payload["name"]
		_, hasNormalizedName := payload["normalized_name"]
		if hasNormalizedName && !hasName {
			return false
		}
		return validRequiredString(payload, "name", 200) && validRequiredString(payload, "normalized_name", 200)
	default:
		return false
	}
}

func validSEOPolicyPayload(payload map[string]any) bool {
	classification, hasClassification := payload["content_classification"]
	quality, hasQuality := payload["seo_quality_status"]
	if !hasClassification && !hasQuality {
		return true
	}
	classificationText, classificationOK := classification.(string)
	qualityText, qualityOK := quality.(string)
	if !hasClassification || !hasQuality || !classificationOK || !qualityOK {
		return false
	}
	if classificationText == "unknown" || qualityText == "unknown" {
		return classificationText == "unknown" && qualityText == "unknown"
	}
	return oneOf(classificationText, "non_explicit", "explicit") && oneOf(qualityText, "eligible", "ineligible")
}

func validRequiredString(payload map[string]any, key string, maximum int) bool {
	value, exists := payload[key]
	if !exists {
		return true
	}
	text, ok := value.(string)
	length := len([]rune(strings.TrimSpace(text)))
	return ok && length >= 1 && length <= maximum
}

func validOptionalString(payload map[string]any, key string, maximum int) bool {
	value, exists := payload[key]
	if !exists || value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && len([]rune(text)) <= maximum
}

func validOptionalUUID(payload map[string]any, key string) bool {
	value, exists := payload[key]
	if !exists || value == nil || value == "" {
		return true
	}
	text, ok := value.(string)
	return ok && uuidPattern.MatchString(text)
}

func validOptionalDate(payload map[string]any, key string) bool {
	value, exists := payload[key]
	if !exists || value == nil || value == "" {
		return true
	}
	text, ok := value.(string)
	if !ok {
		return false
	}
	_, err := time.Parse(time.DateOnly, text)
	return err == nil
}

func validOptionalEnum(payload map[string]any, key string, allowed ...string) bool {
	value, exists := payload[key]
	if !exists {
		return true
	}
	text, ok := value.(string)
	return ok && oneOf(text, allowed...)
}

func validOptionalNumber(payload map[string]any, key string, minimum, maximum int) bool {
	value, exists := payload[key]
	if !exists || value == nil {
		return true
	}
	number, ok := value.(float64)
	return ok && number == float64(int(number)) && int(number) >= minimum && int(number) <= maximum
}

func validPerformerIDPayload(payload map[string]any) bool {
	value, exists := payload["performer_ids"]
	if !exists {
		return true
	}
	if value == nil {
		return true
	}
	items, ok := value.([]any)
	if !ok || len(items) > 20 {
		return false
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return false
		}
		values = append(values, text)
	}
	return validUniqueUUIDs(values)
}

func validPerformerAliasPayload(payload map[string]any) bool {
	value, exists := payload["aliases"]
	if !exists {
		return true
	}
	_, ok := performerAliasesFromPayload(value)
	return ok
}
