package database

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"self-deepsearch/services/platform-api/internal/collectorreview"
)

const collectorCandidateFrom = `
 FROM collector.normalized_records n
 JOIN collector.source_records r USING(source_record_id)
 JOIN collector.publication_batches b ON b.batch_id=r.batch_id
 JOIN collector.sources s ON s.source_id=r.source_id
 WHERE n.parser_version='collector.performers.v1' AND n.entity_type='performer'
 AND b.idempotency_key LIKE 'collector.performers.v1:%'`

const collectorCandidateFilters = `
 AND ($1='' OR r.source_id::text=$1) AND ($2='' OR r.batch_id::text=$2)
 AND ($3='' OR n.candidate_status=$3)
 AND ($4='' OR strpos(lower(r.external_id),lower($4))>0
 OR strpos(lower(r.payload->'metadata'->>'name'),lower($4))>0
 OR EXISTS (SELECT 1 FROM jsonb_array_elements_text(r.payload->'metadata'->'aliases') AS a(value)
 WHERE strpos(lower(a.value),lower($4))>0))`

const collectorCandidateColumns = `SELECT n.normalized_record_id::text, r.source_record_id::text,
 r.batch_id::text, r.source_id::text, s.name, r.external_id, r.payload->'metadata',
 r.content_hash, r.connector_version, r.payload->>'checked_at', r.received_at,
 n.candidate_status, r.payload->>'rights_status', r.payload->>'publication_status', r.payload->>'adult_status'`

func (store *Store) ListCollectorBatches(ctx context.Context, q collectorreview.Query) (collectorreview.BatchPage, error) {
	result := collectorreview.BatchPage{Items: []collectorreview.Batch{}}
	const filter = ` WHERE b.idempotency_key LIKE 'collector.performers.v1:%' AND ($1='' OR b.source_id::text=$1)`
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM collector.publication_batches b`+filter, q.SourceID).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := store.pool.Query(ctx, `SELECT b.batch_id::text,b.source_id::text,s.name,b.region,b.status,
 b.input_count,coalesce((b.summary->>'inserted_count')::int,0),coalesce((b.summary->>'duplicate_count')::int,0),
 (SELECT count(*) FROM collector.source_records r JOIN collector.normalized_records n USING(source_record_id)
 WHERE r.batch_id=b.batch_id AND n.parser_version='collector.performers.v1'),
 (SELECT count(*) FROM collector.source_records r JOIN collector.normalized_records n USING(source_record_id)
 WHERE r.batch_id=b.batch_id AND n.parser_version='collector.performers.v1' AND n.candidate_status='pending'),
 b.request_hash,b.received_at FROM collector.publication_batches b
 JOIN collector.sources s USING(source_id)`+filter+` ORDER BY b.received_at DESC,b.batch_id DESC LIMIT $2 OFFSET $3`, q.SourceID, q.Limit, q.Offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item collectorreview.Batch
		if err := rows.Scan(&item.BatchID, &item.SourceID, &item.SourceName, &item.Region, &item.Status, &item.InputCount,
			&item.InsertedCount, &item.DuplicateCount, &item.CandidateCount, &item.PendingCount, &item.RequestHash, &item.ReceivedAt); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	result.NextOffset = collectorNextOffset(q.Offset, len(result.Items), result.Total)
	return result, rows.Err()
}

func (store *Store) ListCollectorCandidates(ctx context.Context, q collectorreview.Query) (collectorreview.CandidatePage, error) {
	result := collectorreview.CandidatePage{Items: []collectorreview.Candidate{}}
	args := []any{q.SourceID, q.BatchID, q.Status, q.Search}
	if err := store.pool.QueryRow(ctx, `SELECT count(*)`+collectorCandidateFrom+collectorCandidateFilters, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := store.pool.Query(ctx, collectorCandidateColumns+collectorCandidateFrom+collectorCandidateFilters+
		` ORDER BY r.received_at DESC,n.normalized_record_id DESC LIMIT $5 OFFSET $6`, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanCollectorCandidate(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	result.NextOffset = collectorNextOffset(q.Offset, len(result.Items), result.Total)
	return result, rows.Err()
}

func (store *Store) CollectorCandidate(ctx context.Context, id string) (collectorreview.Candidate, error) {
	item, err := scanCollectorCandidate(store.pool.QueryRow(ctx, collectorCandidateColumns+collectorCandidateFrom+
		` AND n.normalized_record_id=$1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, collectorreview.ErrNotFound
	}
	return item, err
}

func scanCollectorCandidate(row pgx.Row) (collectorreview.Candidate, error) {
	var item collectorreview.Candidate
	var raw []byte
	err := row.Scan(&item.CandidateID, &item.SourceRecordID, &item.BatchID, &item.SourceID, &item.SourceName,
		&item.ExternalID, &raw, &item.ContentHash, &item.ConnectorVersion, &item.CheckedAt, &item.ReceivedAt,
		&item.CandidateStatus, &item.RightsStatus, &item.PublicationStatus, &item.AdultStatus)
	if err != nil {
		return item, err
	}
	var metadata struct {
		Name              string   `json:"name"`
		Aliases           []string `json:"aliases"`
		ProfileURL        string   `json:"profile_url"`
		SourceContentHash string   `json:"source_content_hash"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return item, err
	}
	item.Name, item.Aliases, item.ProfileURL, item.SourceContentHash = metadata.Name, metadata.Aliases, metadata.ProfileURL, metadata.SourceContentHash
	if item.Aliases == nil {
		item.Aliases = []string{}
	}
	return item, nil
}

func collectorNextOffset(offset, count int, total int64) *int {
	if count > 0 && int64(offset+count) < total {
		next := offset + count
		return &next
	}
	return nil
}
