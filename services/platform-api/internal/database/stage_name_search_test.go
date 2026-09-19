package database

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/catalog"
)

func TestStageNameSearchPostgres(t *testing.T) {
	dsn := os.Getenv("STAGE_NAME_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable stage-name test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Path != "/self_deepsearch_stage_name_test_20260919" || u.RawQuery != "sslmode=disable" || u.Fragment != "" || u.User == nil || u.User.Username() != "platform_api_login" {
		t.Fatal("refusing writes outside exact restricted local disposable database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := &invitationContractHarness{store: store, ctx: ctx, prefix: "stage-name-" + time.Now().Format("150405.000000000")}
	h.actorID = h.insertUser(t, h.newEmail("author"), "owner", "active")
	reviewer := h.insertUser(t, h.newEmail("reviewer"), "admin", "active")
	first := insertEntitySearchPerformer(t, h, reviewer, "合约公开艺名甲", "契約原文乙", "Stage Contract Alpha", []string{"曾用艺名紫苑合约", "Shared Stage Contract"}, "published")
	second := insertEntitySearchPerformer(t, h, reviewer, "蓝海若叶", "", "", []string{"Shared Stage Contract"}, "published")
	insertEntitySearchPerformer(t, h, reviewer, "合约隐藏人物", "", "", []string{"隐藏艺名不应可见"}, "hidden")
	insertEntitySearchPerformer(t, h, reviewer, "合约下架人物", "", "", []string{"下架艺名不应可见"}, "takedown")
	// Deliberately stale alias text: partial alias lookup must use the actual
	// published entity's alias table, not depend on an old search index snapshot.
	if _, err := store.pool.Exec(ctx, `UPDATE platform.search_documents SET aliases='' WHERE entity_type='performer' AND entity_id IN ($1::uuid,$2::uuid)`, first, second); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		ids   []string
	}{
		{"合约公开艺名甲", []string{first}}, {"曾用艺名紫苑合约", []string{first}},
		{"名紫苑", []string{first}}, {"stage contract alpha", []string{first}},
		{"契約原文乙", []string{first}}, {"Shared Stage Contract", []string{first, second}},
		{"隐藏艺名不应可见", nil}, {"下架艺名不应可见", nil},
	} {
		rows, err := store.SearchPerformers(ctx, tc.query, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		got := performerSearchIDs(rows)
		if len(got) != len(tc.ids) {
			t.Fatalf("query %q got %v want %v", tc.query, got, tc.ids)
		}
		for _, id := range tc.ids {
			if !strings.Contains(strings.Join(got, ","), id) {
				t.Fatalf("query %q missing %s", tc.query, id)
			}
		}
		t.Logf("query=%q hits=%d", tc.query, len(got))
	}
	var after *catalog.SearchCursor
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		rows, err := store.SearchPerformers(ctx, "Shared Stage Contract", after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		if seen[rows[0].ID] {
			t.Fatal("duplicate across cursor pages")
		}
		seen[rows[0].ID] = true
		after = &rows[0].Cursor
	}
	if len(seen) != 2 {
		t.Fatalf("shared stage name must keep separate entities: %v", seen)
	}
	t.Log("same alias preserves two identities; pagination has no duplicate; hidden/takedown excluded")
}
