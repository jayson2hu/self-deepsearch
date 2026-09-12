package mediainspect

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Fixtures and mutations are rolled back. Snapshot queries run under the real
// restricted Worker role; the sibling schedule contract uses its actual LOGIN.
// This is an opt-in SQL contract, not a substitute for a real bucket exercise.
func TestPostgresInspectionSnapshotContract(t *testing.T) {
	runtimeURL, adminURL := os.Getenv("MEDIA_INSPECT_CONTRACT_DATABASE_URL"), os.Getenv("MEDIA_INSPECT_CONTRACT_ADMIN_DATABASE_URL")
	if runtimeURL == "" && adminURL == "" {
		t.Skip("dedicated inspection PostgreSQL contract database is not configured")
	}
	if os.Getenv("CONFIRM_MEDIA_INSPECT_CONTRACT") != "disposable-database" || !sameInspectionContractDatabase(adminURL, runtimeURL) {
		t.Fatal("explicit disposable loopback database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal("cannot open disposable database")
	}
	defer func() { _ = admin.Close(context.Background()) }()
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal("cannot start fixture transaction")
	}
	defer func() {
		rollback, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = tx.Rollback(rollback)
	}()
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('release-a-worker-outbox-contract'))`).Scan(&locked); err != nil || !locked {
		t.Fatal("worker contracts must run serially")
	}
	var version, existing int
	if err := tx.QueryRow(ctx, `SELECT schema_version,
 (SELECT count(*) FROM platform.users)+(SELECT count(*) FROM platform.works)+(SELECT count(*) FROM platform.media_assets)+(SELECT count(*) FROM platform.outbox_events)
 FROM platform.system_metadata WHERE singleton`).Scan(&version, &existing); err != nil || version != 21 || existing != 0 {
		t.Fatal("schema 21 and empty dedicated database required")
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture SQL failed: %v", err)
		}
	}
	var parent, asset string
	if err := tx.QueryRow(ctx, `INSERT INTO platform.works (canonical_code,compact_code,title) VALUES ('INSPECT-1','INSPECT1','Synthetic inspection fixture') RETURNING work_id::text`).Scan(&parent); err != nil {
		t.Fatal("cannot create draft parent")
	}
	if err := tx.QueryRow(ctx, `INSERT INTO platform.media_assets (asset_type,source_type,checked_at,confidence,rights_status,asset_status)
 VALUES ('work_image','fixture',now(),1,'allowed','published') RETURNING asset_id::text`).Scan(&asset); err != nil {
		t.Fatal("cannot create prepared asset")
	}
	exec(`INSERT INTO platform.media_objects (asset_id,version,rendition,storage_provider,storage_scope,storage_key,backup_path,public_url,sha256,mime_type,width,height,byte_size,object_status)
 SELECT $1::uuid,1,rendition,'s3',scope,prefix||$1::text||'/'||rendition||'.webp',prefix||$1::text||'/'||rendition||'.webp',
 CASE WHEN scope='public' THEN 'https://media.example.test/'||prefix||$1::text||'/'||rendition||'.webp' ELSE NULL END,
 repeat('a',64),'image/webp',320,200,100,CASE WHEN scope='public' THEN 'published' ELSE 'ready' END
 FROM (VALUES ('master','private','media-master/'),('w320','public','media-public/'),('w640','public','media-public/'),('w960','public','media-public/')) AS fixture(rendition,scope,prefix)`, asset)
	exec(`INSERT INTO platform.entity_media (entity_type,entity_id,asset_id,purpose,is_primary,publication_status) VALUES ('work',$1::uuid,$2::uuid,'cover',true,'published')`, parent, asset)
	for _, scenario := range []struct {
		name   string
		issues int
	}{
		{"prepared-draft", 0}, {"hidden-parent", 0}, {"shared", 0}, {"retained-private", 0},
		{"old-version", 4}, {"rights-not-withdrawn", 4}, {"missing-public-url", 2}, {"wrong-url-key", 2},
		{"normal-retirement", 0}, {"dead-retirement", 3}, {"deleted-with-url", 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			exec("SAVEPOINT scenario")
			switch scenario.name {
			case "hidden-parent":
				exec(`UPDATE platform.works SET publication_status='hidden' WHERE work_id=$1::uuid`, parent)
			case "shared":
				exec(`INSERT INTO platform.entity_media (entity_type,entity_id,asset_id,purpose,publication_status) VALUES ('work',$1::uuid,$2::uuid,'gallery','published')`, parent, asset)
			case "retained-private":
				exec(`UPDATE platform.media_objects SET object_status='hidden' WHERE asset_id=$1::uuid AND storage_scope='private'`, asset)
			case "old-version":
				exec(`UPDATE platform.media_assets SET current_version=2 WHERE asset_id=$1::uuid`, asset)
			case "rights-not-withdrawn":
				exec(`UPDATE platform.media_assets SET asset_status='takedown',rights_status='takedown' WHERE asset_id=$1::uuid`, asset)
			case "missing-public-url":
				exec(`UPDATE platform.media_objects SET public_url=NULL WHERE asset_id=$1::uuid AND rendition='w320'`, asset)
			case "wrong-url-key":
				exec(`UPDATE platform.media_objects SET public_url='https://media.example.test/media-public/wrong.webp' WHERE asset_id=$1::uuid AND rendition='w320'`, asset)
			case "deleted-with-url":
				exec(`UPDATE platform.media_objects SET object_status='deleted',deleted_at=now() WHERE asset_id=$1::uuid AND rendition='w320'`, asset)
			case "normal-retirement", "dead-retirement":
				exec(`INSERT INTO platform.outbox_events (aggregate_type,aggregate_id,event_type,payload,dedupe_key)
 SELECT 'media',asset_id,'media_delete',jsonb_build_object('media_object_id',media_object_id,'asset_id',asset_id,'storage_scope',storage_scope,'storage_key',storage_key,'backup_path',backup_path,'public_url',public_url),
 'media-delete:'||media_object_id::text FROM platform.media_objects WHERE asset_id=$1::uuid AND storage_scope='public'`, asset)
				exec(`UPDATE platform.entity_media SET publication_status='hidden' WHERE asset_id=$1::uuid`, asset)
				exec(`UPDATE platform.media_assets SET asset_status='hidden' WHERE asset_id=$1::uuid`, asset)
				exec(`UPDATE platform.media_objects SET object_status='hidden',public_url=NULL WHERE asset_id=$1::uuid`, asset)
				if scenario.name == "dead-retirement" {
					exec(`UPDATE platform.outbox_events SET status='dead' WHERE aggregate_id=$1::uuid`, asset)
				}
			}
			exec("SET LOCAL ROLE platform_worker_login")
			var restricted bool
			if err := tx.QueryRow(ctx, `SELECT current_user='platform_worker_login' AND NOT rolsuper AND NOT rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&restricted); err != nil || !restricted {
				t.Fatal("restricted worker role required")
			}
			snapshot, err := readSnapshot(ctx, tx)
			if err != nil {
				t.Fatalf("restricted inventory query failed: %v", err)
			}
			report := Inspect(snapshot)
			if report.ObjectCount != 4 || report.PublicationIssues != scenario.issues {
				t.Fatalf("%s: %+v", scenario.name, report)
			}
			exec("RESET ROLE")
			exec("ROLLBACK TO SAVEPOINT scenario")
		})
	}
}
