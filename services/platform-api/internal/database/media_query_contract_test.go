package database

import (
	"strings"
	"testing"
)

func TestEntityImagesQueryDefendsPublishedDisplaySlots(t *testing.T) {
	for _, clause := range []string{
		"purpose = 'cover' AND is_primary AND position = 0",
		"purpose = 'gallery' AND NOT is_primary AND position BETWEEN 1 AND 3",
		"purpose = 'avatar' AND is_primary AND position = 0",
		"EXISTS (SELECT 1 FROM ranked_assets AS primary_asset WHERE primary_asset.is_primary)",
		"LIMIT 4",
	} {
		if !strings.Contains(entityImagesQuery, clause) {
			t.Fatalf("public media query lost display-slot guard %q", clause)
		}
	}
	if strings.Count(entityImagesQuery, "FROM platform.public_entity_media") != 1 {
		t.Fatal("public media rows must flow through the eligible_media guard")
	}
}

func TestSummaryQueriesOnlyUseValidPrimarySlots(t *testing.T) {
	for name, contract := range map[string]struct {
		query   string
		purpose string
	}{
		"work":      {query: workSummaryJoins, purpose: "cover"},
		"performer": {query: performerSummaryJoins, purpose: "avatar"},
	} {
		for _, clause := range []string{"AND purpose = '" + contract.purpose + "'", "AND is_primary", "AND position = 0"} {
			if !strings.Contains(contract.query, clause) {
				t.Fatalf("%s summary query lost primary-slot guard %q", name, clause)
			}
		}
	}
}
