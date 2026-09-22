package database

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/jackc/pgx/v5"
	"self-deepsearch/services/platform-api/internal/collectoringest"
)

// IngestPerformers writes observations and pending candidates only. The advisory source lock
// serializes overlapping batches so duplicate observations never overwrite review state.
func (store *Store) IngestPerformers(ctx context.Context, b collectoringest.Batch, hash string) (collectoringest.Receipt, bool, error) {
	var receipt collectoringest.Receipt
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return receipt, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'; SET LOCAL statement_timeout = '10s'`); err != nil {
		return receipt, false, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 20260922))`, b.SourceID); err != nil {
		return receipt, false, err
	}
	var baseURL string
	err = tx.QueryRow(ctx, `SELECT base_url FROM collector.sources WHERE source_id=$1::uuid AND status='active' AND source_type='public_web' AND rights_status IN ('needs_review','allowed') AND base_url IS NOT NULL`, b.SourceID).Scan(&baseURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return receipt, false, collectoringest.ErrSource
	}
	if err != nil {
		return receipt, false, err
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" {
		return receipt, false, collectoringest.ErrSource
	}
	for _, r := range b.Records {
		u, err := url.Parse(r.Payload.ProfileURL)
		if err != nil || u.Host != base.Host {
			return receipt, false, collectoringest.ErrSource
		}
	}
	// Prefix isolates service batches from the operator CSV import namespace.
	key := "collector.performers.v1:" + b.SourceID + ":" + b.IdempotencyKey
	var existingHash string
	var saved []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,summary FROM collector.publication_batches WHERE idempotency_key=$1`, key).Scan(&existingHash, &saved)
	if err == nil {
		if existingHash != hash {
			return receipt, false, collectoringest.ErrConflict
		}
		if err = json.Unmarshal(saved, &receipt); err != nil {
			return receipt, false, err
		}
		return receipt, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return receipt, false, err
	}
	receipt.RequestHash, receipt.Status, receipt.InputCount = hash, "staged", len(b.Records)
	err = tx.QueryRow(ctx, `INSERT INTO collector.publication_batches(batch_id,source_id,region,idempotency_key,request_hash,input_count,status) VALUES(gen_random_uuid(),$1::uuid,$2,$3,$4,$5,'receiving') RETURNING batch_id::text,received_at`, b.SourceID, b.Region, key, hash, len(b.Records)).Scan(&receipt.BatchID, &receipt.ReceivedAt)
	if err != nil {
		return receipt, false, err
	}
	for _, r := range b.Records {
		// checked_at is evidence, not a material content change. The upstream hash is
		// retained in the payload; our hash describes the exact typed payload we store.
		payload, _ := json.Marshal(r.Payload)
		contentHash := collectoringest.Hash(payload)
		observationKey := "performer-v1:" + collectoringest.Hash([]byte(r.ExternalID+":"+contentHash))
		var recordID string
		err = tx.QueryRow(ctx, `INSERT INTO collector.source_records(batch_id,source_id,external_id,entity_type,connector_version,idempotency_key,content_hash,payload) VALUES($1::uuid,$2::uuid,$3,'performer',$4,$5,$6,jsonb_build_object('metadata',$7::jsonb,'checked_at',$8::text,'rights_status','needs_review','publication_status','staging','adult_status','unknown')) ON CONFLICT (source_id,entity_type,external_id,content_hash) DO NOTHING RETURNING source_record_id::text`, receipt.BatchID, b.SourceID, r.ExternalID, b.ConnectorVersion, observationKey, contentHash, payload, r.CheckedAt).Scan(&recordID)
		if errors.Is(err, pgx.ErrNoRows) {
			receipt.DuplicateCount++
			continue
		}
		if err != nil {
			return receipt, false, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO collector.normalized_records(source_record_id,parser_version,entity_type,normalized_payload,candidate_status) SELECT source_record_id,'collector.performers.v1','performer',payload,'pending' FROM collector.source_records WHERE source_record_id=$1::uuid`, recordID)
		if err != nil {
			return receipt, false, err
		}
		receipt.InsertedCount++
	}
	saved, _ = json.Marshal(receipt)
	_, err = tx.Exec(ctx, `UPDATE collector.publication_batches SET status='accepted',accepted_count=input_count,summary=$2::jsonb,completed_at=now() WHERE batch_id=$1::uuid`, receipt.BatchID, saved)
	if err != nil {
		return receipt, false, err
	}
	err = tx.Commit(ctx)
	return receipt, false, err
}
