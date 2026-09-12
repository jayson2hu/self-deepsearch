package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

const discoveryMixRuleAuditID = "70000000-0000-4000-8000-000000000002"

func (store *Store) GetDiscoveryMixRule(ctx context.Context) (operations.DiscoveryMixRule, error) {
	return discoveryMixRuleWith(ctx, store.pool)
}

func (store *Store) UpdateDiscoveryMixRule(ctx context.Context, input operations.DiscoveryMixRuleInput, actorID, requestID string, now time.Time) (operations.DiscoveryMixRule, error) {
	if input.WorkSlots < 1 || input.WorkSlots > 8 || input.PerformerSlots < 1 || input.PerformerSlots > 4 ||
		input.RepeatWindow < 5 || input.RepeatWindow > 40 {
		return operations.DiscoveryMixRule{}, operations.ErrInvalidInput
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.DiscoveryMixRule{}, fmt.Errorf("begin discovery mix update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := discoveryMixRuleWith(ctx, tx)
	if err != nil {
		return operations.DiscoveryMixRule{}, err
	}
	command, err := tx.Exec(ctx, `
UPDATE platform.site_content_rules
SET value = jsonb_build_object('work_slots', $1::integer, 'performer_slots', $2::integer, 'repeat_window', $3::integer),
    enabled = $4, updated_by = $5::uuid, updated_at = $6
WHERE rule_key = 'home.discovery_mix'`, input.WorkSlots, input.PerformerSlots, input.RepeatWindow,
		input.Enabled, actorID, now)
	if err != nil {
		return operations.DiscoveryMixRule{}, mapConstraintError(err)
	}
	if command.RowsAffected() != 1 {
		return operations.DiscoveryMixRule{}, operations.ErrNotFound
	}
	after := operations.DiscoveryMixRule{WorkSlots: input.WorkSlots, PerformerSlots: input.PerformerSlots,
		RepeatWindow: input.RepeatWindow, Enabled: input.Enabled, UpdatedAt: now}
	if err := insertAudit(ctx, tx, actorID, "site_content_rule.update", "site_content_rule",
		discoveryMixRuleAuditID, before, after, input.Reason, requestID, now); err != nil {
		return operations.DiscoveryMixRule{}, err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ('site_content_rule', $1::uuid, 'cache_purge',
        jsonb_build_object('reason', 'discovery_mix_changed'), $1::text || ':' || $2)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, discoveryMixRuleAuditID, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return operations.DiscoveryMixRule{}, fmt.Errorf("enqueue discovery mix cache invalidation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.DiscoveryMixRule{}, fmt.Errorf("commit discovery mix update: %w", err)
	}
	return after, nil
}

type discoveryRuleQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func discoveryMixRuleWith(ctx context.Context, queryer discoveryRuleQueryer) (operations.DiscoveryMixRule, error) {
	var item operations.DiscoveryMixRule
	err := queryer.QueryRow(ctx, `
SELECT (value->>'work_slots')::integer, (value->>'performer_slots')::integer,
       (value->>'repeat_window')::integer, enabled, updated_at
FROM platform.site_content_rules WHERE rule_key = 'home.discovery_mix'`).Scan(
		&item.WorkSlots, &item.PerformerSlots, &item.RepeatWindow, &item.Enabled, &item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.DiscoveryMixRule{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.DiscoveryMixRule{}, fmt.Errorf("read discovery mix rule: %w", err)
	}
	return item, nil
}
