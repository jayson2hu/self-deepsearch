package database

import (
	"fmt"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/catalog"
)

func sitemapSlugs(items []catalog.SitemapEntry) []string {
	slugs := make([]string, 0, len(items))
	for _, item := range items {
		slugs = append(slugs, item.Slug)
	}
	return slugs
}

func TestSitemapPaginationAndVisibilityPostgres(t *testing.T) {
	h := newInvitationContractHarness(t)
	reviewerID := h.insertUser(t, h.newEmail("sitemap-reviewer"), "admin", "active")
	publishedIDs := make([]string, 0, 26)
	for index := 1; index <= 26; index++ {
		publishedIDs = append(publishedIDs, insertEntitySearchStudio(t, h, reviewerID, fmt.Sprintf("DEV13 地图厂牌 %02d", index), "published"))
	}
	hiddenID := insertEntitySearchStudio(t, h, reviewerID, "DEV13 隐藏厂牌", "hidden")
	takedownID := insertEntitySearchStudio(t, h, reviewerID, "DEV13 下架厂牌", "takedown")
	if hiddenID == "" || takedownID == "" {
		t.Fatal("hidden/takedown sitemap fixtures were not created")
	}

	wantUpdated := time.Date(2026, 9, 16, 8, 30, 0, 0, time.UTC)
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.publications SET updated_at = $1 WHERE entity_type = 'studio' AND entity_id = $2::uuid`, wantUpdated, publishedIDs[0]); err != nil {
		t.Fatalf("set sitemap lastmod fixture: %v", err)
	}
	if err := h.store.pool.QueryRow(h.ctx, `SELECT updated_at FROM platform.public_published_studios WHERE studio_id = $1::uuid`, publishedIDs[0]).Scan(&wantUpdated); err != nil {
		t.Fatalf("read trigger-maintained public lastmod: %v", err)
	}

	total, err := h.store.SitemapCount(h.ctx, "studio")
	if err != nil {
		t.Fatalf("count public studio sitemap: %v", err)
	}
	all, err := h.store.SitemapEntries(h.ctx, "studio", 0, total+1)
	if err != nil || len(all) != total {
		t.Fatalf("load complete sitemap: count=%d items=%d err=%v", total, len(all), err)
	}

	paged := make([]catalog.SitemapEntry, 0, total)
	for offset := 0; offset < total; offset += 17 {
		page, pageErr := h.store.SitemapEntries(h.ctx, "studio", offset, 17)
		if pageErr != nil {
			t.Fatalf("load sitemap page at offset %d: %v", offset, pageErr)
		}
		paged = append(paged, page...)
	}
	if fmt.Sprint(sitemapSlugs(paged)) != fmt.Sprint(sitemapSlugs(all)) {
		t.Fatal("sitemap pagination has duplicates, gaps, or unstable ordering")
	}

	var firstPublishedSlug, hiddenSlug, takedownSlug string
	for id, target := range map[string]*string{publishedIDs[0]: &firstPublishedSlug, hiddenID: &hiddenSlug, takedownID: &takedownSlug} {
		if err := h.store.pool.QueryRow(h.ctx, `SELECT canonical_slug FROM platform.studios WHERE studio_id = $1::uuid`, id).Scan(target); err != nil {
			t.Fatalf("read sitemap fixture slug: %v", err)
		}
	}
	foundUpdated := false
	for _, item := range all {
		if item.Slug == hiddenSlug || item.Slug == takedownSlug {
			t.Fatalf("non-public entity leaked into sitemap: %s", item.Slug)
		}
		if item.Slug == firstPublishedSlug {
			foundUpdated = item.UpdatedAt.Equal(wantUpdated)
		}
	}
	if !foundUpdated {
		t.Fatal("sitemap lastmod did not use the public publication updated_at")
	}

	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.studios SET publication_status = 'hidden' WHERE studio_id = $1::uuid`, publishedIDs[0]); err != nil {
		t.Fatalf("hide sitemap entity: %v", err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.publications SET publication_status = 'hidden', hidden_at = now(), updated_at = now() WHERE entity_type = 'studio' AND entity_id = $1::uuid`, publishedIDs[0]); err != nil {
		t.Fatalf("hide sitemap publication: %v", err)
	}
	afterCount, err := h.store.SitemapCount(h.ctx, "studio")
	if err != nil || afterCount != total-1 {
		t.Fatalf("hidden sitemap count=%d, want %d, err=%v", afterCount, total-1, err)
	}
	after, err := h.store.SitemapEntries(h.ctx, "studio", 0, afterCount+1)
	if err != nil {
		t.Fatalf("load sitemap after hide: %v", err)
	}
	for _, item := range after {
		if item.Slug == firstPublishedSlug {
			t.Fatal("hidden entity remained in sitemap")
		}
	}
}
