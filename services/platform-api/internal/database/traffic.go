package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"self-deepsearch/services/platform-api/internal/operations"
)

// Reuse existing aggregates/history. Anonymous identifiers are neither created
// nor inferred from IPs, sessions, PV or registered-user counts.
func (store *Store) TrafficReport(ctx context.Context, period string, now time.Time) (operations.TrafficReport, error) {
	if period != "7d" && period != "30d" && period != "all" {
		return operations.TrafficReport{}, operations.ErrInvalidInput
	}
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	local := now.In(zone)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	days := 30
	if period == "7d" {
		days = 7
	}
	start := today.AddDate(0, 0, -days+1)
	var cutoff any = start
	if period == "all" {
		cutoff = nil
	}
	result := operations.TrafficReport{Range: period, CheckedAt: now, Timezone: "Asia/Shanghai",
		Daily: []operations.TrafficDay{}, HotContent: []operations.TrafficContent{}, Users: []operations.TrafficUser{}}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, fmt.Errorf("begin traffic snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `SELECT
 coalesce(sum(page_views) FILTER(WHERE $1::timestamptz IS NULL OR hour_bucket >= $1),0)::bigint,
 coalesce(sum(page_views) FILTER(WHERE hour_bucket >= $2),0)::bigint
 FROM platform.content_metrics_hourly WHERE hour_bucket <= $3`, cutoff, today, now).Scan(&result.PageViews, &result.TodayPageViews)
	if err != nil {
		return result, fmt.Errorf("traffic totals: %w", err)
	}
	// Cleared visible histories remain counted: clearing the personal history UI
	// is not a request to subtract historical aggregate visits. Test/management and
	// closed accounts are excluded from registered-user analytics.
	const usersCTE = `WITH visits AS (
 SELECT u.user_id,u.normalized_email,count(*)::bigint views FROM platform.view_history h
 JOIN platform.users u ON u.user_id=h.user_id
 WHERE u.role='user' AND NOT u.is_test AND u.account_status <> 'closed'
 AND ($1::timestamptz IS NULL OR h.viewed_at >= $1) AND h.viewed_at <= $2
 GROUP BY u.user_id,u.normalized_email)`
	err = tx.QueryRow(ctx, usersCTE+` SELECT coalesce(sum(views),0)::bigint,count(*) FROM visits`, cutoff, now).Scan(&result.LoggedInUserViews, &result.ActiveRegisteredUsers)
	if err != nil {
		return result, fmt.Errorf("registered traffic: %w", err)
	}
	rows, err := tx.Query(ctx, `WITH days AS (
 SELECT generate_series($1::date,$2::date,interval '1 day')::date date_day
 ), measured AS (
 SELECT (hour_bucket AT TIME ZONE 'Asia/Shanghai')::date date_day,sum(page_views)::bigint views
 FROM platform.content_metrics_hourly WHERE hour_bucket >= $3 AND hour_bucket <= $4 GROUP BY 1
 ) SELECT to_char(days.date_day,'YYYY-MM-DD'),coalesce(measured.views,0) FROM days LEFT JOIN measured USING(date_day) ORDER BY days.date_day`,
		start.Format(time.DateOnly), today.Format(time.DateOnly), start, now)
	if err != nil {
		return result, fmt.Errorf("traffic daily: %w", err)
	}
	for rows.Next() {
		var day operations.TrafficDay
		if err = rows.Scan(&day.Date, &day.PageViews); err != nil {
			rows.Close()
			return result, err
		}
		result.Daily = append(result.Daily, day)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = tx.Query(ctx, `WITH catalog AS (
 SELECT 'work'::text entity_type,work_id id,canonical_code||' / '||title title,'/works/'||canonical_slug path FROM platform.public_published_works
 UNION ALL SELECT 'performer',performer_id,display_name,'/performers/'||canonical_slug FROM platform.public_published_performers
 UNION ALL SELECT 'studio',studio_id,name,'/studios/'||canonical_slug FROM platform.public_published_studios
 ), ranked AS (
 SELECT c.entity_type,c.id,c.title,c.path,sum(m.page_views)::bigint views
 FROM catalog c JOIN platform.content_metrics_hourly m ON m.content_type=c.entity_type AND m.content_id=c.id
 WHERE ($1::timestamptz IS NULL OR m.hour_bucket >= $1) AND m.hour_bucket <= $2
 GROUP BY c.entity_type,c.id,c.title,c.path HAVING sum(m.page_views)>0
 ) SELECT entity_type,id::text,title,path,views FROM ranked ORDER BY views DESC,entity_type,id LIMIT 20`, cutoff, now)
	if err != nil {
		return result, fmt.Errorf("traffic ranking: %w", err)
	}
	for rows.Next() {
		var item operations.TrafficContent
		if err = rows.Scan(&item.EntityType, &item.ID, &item.Title, &item.Path, &item.PageViews); err != nil {
			rows.Close()
			return result, err
		}
		item.Rank = len(result.HotContent) + 1
		result.HotContent = append(result.HotContent, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = tx.Query(ctx, usersCTE+` SELECT user_id::text,normalized_email,views FROM visits ORDER BY views DESC,user_id LIMIT 50`, cutoff, now)
	if err != nil {
		return result, fmt.Errorf("traffic users: %w", err)
	}
	for rows.Next() {
		var item operations.TrafficUser
		if err = rows.Scan(&item.ID, &item.Email, &item.PageViews); err != nil {
			rows.Close()
			return result, err
		}
		result.Users = append(result.Users, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
