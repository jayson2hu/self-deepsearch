package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

func TestPostgresSEOIssueWorkflowContract(t *testing.T) {
	databaseURL := os.Getenv("SEO_WORKFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SEO_WORKFLOW_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var actorID, reviewerID string
	if err := store.pool.QueryRow(ctx, `SELECT user_id::text FROM platform.users WHERE account_status='active' AND role IN ('owner','admin') ORDER BY user_id LIMIT 1`).Scan(&actorID); err != nil {
		t.Fatalf("requires a synthetic management user: %v", err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT user_id::text FROM platform.users WHERE account_status='active' AND user_id<>$1::uuid ORDER BY user_id LIMIT 1`, actorID).Scan(&reviewerID); err != nil || actorID == reviewerID {
		t.Fatalf("requires two synthetic active users: %v", err)
	}
	const workID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	const revisionID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	scanIDs := make([]string, 0, 3)
	if _, err := store.pool.Exec(ctx, `INSERT INTO platform.works(work_id,canonical_code,compact_code,title,publication_status)
VALUES ($1::uuid,'SEO-CYCLE-001','SEOCYCLE001','SEO 闭环测试','published')`, workID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO platform.content_revisions(revision_id,entity_type,entity_id,version,payload,revision_status,author_id,reviewer_id,reason,submitted_at,reviewed_at)
VALUES ($2::uuid,'work',$1::uuid,1,'{}','approved',$3::uuid,$4::uuid,'SEO workflow contract',$5,$5)`, workID, revisionID, actorID, reviewerID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO platform.publications(entity_type,entity_id,current_revision_id,publication_status,canonical_slug,published_at)
VALUES ('work',$1::uuid,$2::uuid,'published','seo-cycle-001',$3)`, workID, revisionID, now); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup := context.Background()
		_, _ = store.pool.Exec(cleanup, `DELETE FROM platform.seo_issue_events WHERE issue_id IN (SELECT issue_id FROM platform.seo_issues WHERE entity_id=$1::uuid)`, workID)
		_, _ = store.pool.Exec(cleanup, `DELETE FROM platform.seo_issues WHERE entity_id=$1::uuid`, workID)
		_, _ = store.pool.Exec(cleanup, `DELETE FROM platform.seo_scan_runs WHERE scan_id=ANY($1::uuid[])`, scanIDs)
		_, _ = store.pool.Exec(cleanup, `DELETE FROM platform.publications WHERE entity_type='work' AND entity_id=$1::uuid`, workID)
		_, _ = store.pool.Exec(cleanup, `DELETE FROM platform.content_revisions WHERE revision_id=$1::uuid`, revisionID)
		_, _ = store.pool.Exec(cleanup, `DELETE FROM platform.works WHERE work_id=$1::uuid`, workID)
	}()

	first, err := store.RunSEOScan(ctx, actorID, "seo-contract-first", now)
	if err != nil || first.IssueCount < 1 || first.ScannedCount > 1000 {
		t.Fatalf("first bounded scan: %+v %v", first, err)
	}
	scanIDs = append(scanIDs, first.ID)
	repeated, err := store.RunSEOScan(ctx, actorID, "seo-contract-repeat", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	scanIDs = append(scanIDs, repeated.ID)
	issues, err := store.ListSEOIssues(ctx, "open", 100)
	if err != nil {
		t.Fatal(err)
	}
	var issue operations.SEOIssue
	for _, candidate := range issues {
		if candidate.EntityID == workID {
			issue = candidate
		}
	}
	if issue.ID == "" || issue.RuleCode != "SEO_CLASSIFICATION_UNKNOWN" {
		t.Fatalf("active issue was not deduplicated: %+v", issue)
	}
	update := operations.SEOIssueUpdate{Status: "in_progress", AssigneeID: &actorID, Note: "开始修复分类"}
	issue, err = store.UpdateSEOIssue(ctx, issue.ID, update, actorID, "seo-contract-progress", now.Add(2*time.Minute))
	if err != nil || issue.Status != "in_progress" {
		t.Fatalf("start issue: %+v %v", issue, err)
	}
	update.Status, update.Note = "retest", "提交复测"
	issue, err = store.UpdateSEOIssue(ctx, issue.ID, update, actorID, "seo-contract-retest", now.Add(3*time.Minute))
	if err != nil || issue.Status != "retest" {
		t.Fatalf("retest issue: %+v %v", issue, err)
	}
	update.Status, update.Note = "closed", "尝试在未修复时关闭"
	if _, err := store.UpdateSEOIssue(ctx, issue.ID, update, actorID, "seo-contract-close-fail", now.Add(4*time.Minute)); !errors.Is(err, operations.ErrConflict) {
		t.Fatalf("unresolved close = %v, want conflict", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE platform.publications SET content_classification='non_explicit',seo_quality_status='eligible',seo_reviewed_at=$2,seo_reviewed_by=$3::uuid WHERE entity_id=$1::uuid`, workID, now, reviewerID); err != nil {
		t.Fatal(err)
	}
	update.Status, update.Note = "retest", "修复后再次复测"
	if _, err := store.UpdateSEOIssue(ctx, issue.ID, update, actorID, "seo-contract-retest-2", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	update.Status, update.Note = "closed", "资格规则复测通过"
	closed, err := store.UpdateSEOIssue(ctx, issue.ID, update, actorID, "seo-contract-close", now.Add(6*time.Minute))
	if err != nil || closed.Status != "closed" || closed.ClosedAt == nil {
		t.Fatalf("close resolved issue: %+v %v", closed, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE platform.publications SET content_classification='unknown',seo_quality_status='unknown',seo_reviewed_at=NULL,seo_reviewed_by=NULL WHERE entity_id=$1::uuid`, workID); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.RunSEOScan(ctx, actorID, "seo-contract-reopen", now.Add(7*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	scanIDs = append(scanIDs, reopened.ID)
	var total, active int
	if err := store.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE status<>'closed') FROM platform.seo_issues WHERE entity_id=$1::uuid`, workID).Scan(&total, &active); err != nil || total != 2 || active != 1 {
		t.Fatalf("rediscovery did not preserve closed history: total=%d active=%d err=%v", total, active, err)
	}
}
