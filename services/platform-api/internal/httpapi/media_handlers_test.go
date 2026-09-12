package httpapi

import "testing"

func TestValidRenditionURLRequiresConfiguredMediaOrigin(t *testing.T) {
	value := "https://media.example.test/media-public/default/works/a.webp"
	if !validRenditionURL("w320", &value, "media-public/default/works/a.webp", "https://media.example.test") {
		t.Fatal("configured media origin and matching key should be accepted")
	}
	if validRenditionURL("w320", &value, "media-public/default/works/a.webp", "https://other.example.test") {
		t.Fatal("a different public media origin must be rejected")
	}
	if validRenditionURL("master", &value, "media-master/a.webp", "https://media.example.test") {
		t.Fatal("master objects must never have a public URL")
	}
}

func TestValidMediaIdentityRequiresOnePrimaryOrThreeGallerySlots(t *testing.T) {
	valid := []mediaManifestRequest{
		{EntityType: "work", AssetType: "work_image", Purpose: "cover", Position: 0, IsPrimary: true},
		{EntityType: "work", AssetType: "work_image", Purpose: "gallery", Position: 1, IsPrimary: false},
		{EntityType: "work", AssetType: "work_image", Purpose: "gallery", Position: 3, IsPrimary: false},
		{EntityType: "performer", AssetType: "performer_avatar", Purpose: "avatar", Position: 0, IsPrimary: true},
	}
	for _, request := range valid {
		if !validMediaIdentity(request) {
			t.Fatalf("valid display slot was rejected: %#v", request)
		}
	}
	invalid := []mediaManifestRequest{
		{EntityType: "work", AssetType: "work_image", Purpose: "cover", Position: 0, IsPrimary: false},
		{EntityType: "work", AssetType: "work_image", Purpose: "cover", Position: 1, IsPrimary: true},
		{EntityType: "work", AssetType: "work_image", Purpose: "gallery", Position: 1, IsPrimary: true},
		{EntityType: "work", AssetType: "work_image", Purpose: "gallery", Position: 0, IsPrimary: false},
		{EntityType: "work", AssetType: "work_image", Purpose: "gallery", Position: 4, IsPrimary: false},
		{EntityType: "performer", AssetType: "performer_avatar", Purpose: "avatar", Position: 0, IsPrimary: false},
		{EntityType: "performer", AssetType: "performer_avatar", Purpose: "avatar", Position: 1, IsPrimary: true},
	}
	for _, request := range invalid {
		if validMediaIdentity(request) {
			t.Fatalf("invalid display slot was accepted: %#v", request)
		}
	}
}
