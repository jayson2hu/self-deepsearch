package database

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

func TestPostgresSEOIssueWorkflowContract(t *testing.T) {
	databaseURL := os.Getenv("SEO_WORKFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SEO_WORKFLOW_TEST_DATABASE_URL is not set")
	}
	if !validIdentityContractURL(databaseURL) {
		t.Fatal("requires isolated migration test database with restricted role")
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
	var workID, revisionID string
	if err := store.pool.QueryRow(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&workID, &revisionID); err != nil {
		t.Fatal(err)
	}
	code := "SEO-" + strings.ReplaceAll(workID, "-", "")
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	if _, err := store.pool.Exec(ctx, `INSERT INTO platform.works(work_id,canonical_code,compact_code,title,publication_status)
VALUES ($1::uuid,$2,$3,'SEO 闭环测试','published')`, workID, code, compactIdentifier(code)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO platform.content_revisions(revision_id,entity_type,entity_id,version,payload,revision_status,author_id,reviewer_id,reason,submitted_at,reviewed_at)
VALUES ($2::uuid,'work',$1::uuid,1,'{}','approved',$3::uuid,$4::uuid,'SEO workflow contract',$5,$5)`, workID, revisionID, actorID, reviewerID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO platform.publications(entity_type,entity_id,current_revision_id,publication_status,canonical_slug,published_at)
VALUES ('work',$1::uuid,$2::uuid,'published',$4,$3)`, workID, revisionID, now, strings.ToLower(code)); err != nil {
		t.Fatal(err)
	}
	// Keep append-only evidence in this disposable DB; never broaden runtime
	// privileges for test cleanup. Random identifiers make reruns independent.

	first, err := store.RunSEOScan(ctx, actorID, "seo-contract-first", now)
	if err != nil || first.IssueCount < 1 || first.ScannedCount > 1000 {
		t.Fatalf("first bounded scan: %+v %v", first, err)
	}
	_, err = store.RunSEOScan(ctx, actorID, "seo-contract-repeat", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// A large catalog may put this fixture beyond the first 100 issues.
	var issueID string
	var issueCount int
	if err := store.pool.QueryRow(ctx, `SELECT min(issue_id::text),count(*) FROM platform.seo_issues WHERE entity_id=$1::uuid AND status='open'`, workID).Scan(&issueID, &issueCount); err != nil || issueCount != 1 {
		t.Fatalf("active issue was not deduplicated: count=%d err=%v", issueCount, err)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := loadSEOIssue(ctx, tx, issueID)
	_ = tx.Rollback(ctx)
	if err != nil || issue.RuleCode != "SEO_CLASSIFICATION_UNKNOWN" {
		t.Fatalf("unexpected issue: %+v err=%v", issue, err)
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
	_, err = store.RunSEOScan(ctx, actorID, "seo-contract-reopen", now.Add(7*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var total, active int
	if err := store.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE status<>'closed') FROM platform.seo_issues WHERE entity_id=$1::uuid`, workID).Scan(&total, &active); err != nil || total != 2 || active != 1 {
		t.Fatalf("rediscovery did not preserve closed history: total=%d active=%d err=%v", total, active, err)
	}
}
