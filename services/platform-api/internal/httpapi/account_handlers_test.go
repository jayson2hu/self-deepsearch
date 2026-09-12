package httpapi

import "testing"

func TestValidFeedbackTarget(t *testing.T) {
	validID := "10000000-0000-4000-8000-000000000001"
	invalidID := "not-a-uuid"
	invalidType := "media"

	tests := []struct {
		name        string
		contentType *string
		contentID   *string
		valid       bool
	}{
		{name: "general feedback", valid: true},
		{name: "work target", contentType: feedbackStringPtr("work"), contentID: &validID, valid: true},
		{name: "performer target", contentType: feedbackStringPtr("performer"), contentID: &validID, valid: true},
		{name: "studio target", contentType: feedbackStringPtr("studio"), contentID: &validID, valid: true},
		{name: "missing content id", contentType: feedbackStringPtr("work"), valid: false},
		{name: "missing content type", contentID: &validID, valid: false},
		{name: "unknown content type", contentType: &invalidType, contentID: &validID, valid: false},
		{name: "malformed content id", contentType: feedbackStringPtr("work"), contentID: &invalidID, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := validFeedbackTarget(test.contentType, test.contentID); actual != test.valid {
				t.Fatalf("validFeedbackTarget(%v, %v) = %t, want %t", test.contentType, test.contentID, actual, test.valid)
			}
		})
	}
}

func feedbackStringPtr(value string) *string { return &value }
