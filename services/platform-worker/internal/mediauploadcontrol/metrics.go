package mediauploadcontrol

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
)

func (q *PostgresQueue) WriteMetrics(ctx context.Context, w io.Writer, now time.Time) {
	readable, fresh, open, uncertain := 0, 0, 0, 0
	var success *time.Time
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tx, err := q.begin(bounded, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err == nil {
		defer func() { _ = tx.Rollback(bounded) }()
		var code *string
		err = tx.QueryRow(bounded, `SELECT last_success_at,last_error_code,
 (SELECT count(*) FROM audit.media_upload_commands WHERE status IN ('pending','running','uncertain')),
 (SELECT count(*) FROM audit.media_upload_commands WHERE status='uncertain')
 FROM platform.media_upload_control_state WHERE singleton AND target_ref=$1`, q.target).Scan(&success, &code, &open, &uncertain)
		if err == nil && tx.Commit(bounded) == nil {
			readable = 1
			if code == nil && success != nil && !now.Before(*success) && now.Sub(*success) <= 2*time.Minute {
				fresh = 1
			}
		}
	}
	if readable == 0 {
		open, uncertain = 0, 0
	}
	_, _ = fmt.Fprintf(w, "# TYPE self_deepsearch_media_upload_queue_enabled gauge\nself_deepsearch_media_upload_queue_enabled 1\n"+
		"# TYPE self_deepsearch_media_upload_queue_state_readable gauge\nself_deepsearch_media_upload_queue_state_readable %d\n"+
		"# TYPE self_deepsearch_media_upload_queue_snapshot_fresh gauge\nself_deepsearch_media_upload_queue_snapshot_fresh %d\n"+
		"# TYPE self_deepsearch_media_upload_queue_open_commands gauge\nself_deepsearch_media_upload_queue_open_commands %d\n"+
		"# TYPE self_deepsearch_media_upload_queue_uncertain_commands gauge\nself_deepsearch_media_upload_queue_uncertain_commands %d\n", readable, fresh, open, uncertain)
}
