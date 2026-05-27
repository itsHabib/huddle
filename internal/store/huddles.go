package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/types"
)

// DefaultOrchestratorID is the sentinel persisted when a caller doesn't
// supply a stable orchestrator identifier. It must match the DEFAULT
// clause on `huddles.orchestrator_id` in schema.sql and the backfill DDL
// in db.go so legacy rows, fresh CREATE TABLE rows, and Go-side defaults
// all agree.
const DefaultOrchestratorID = "orchestrator"

// DefaultOrchestratorDisplayName mirrors DefaultOrchestratorID for the
// display-name column. Same agreement requirement with schema.sql.
const DefaultOrchestratorDisplayName = "orchestrator"

// InsertHuddle persists a brand-new huddle row. Defaults empty
// orchestrator fields to their sentinels so callers building a
// types.Huddle directly (bypassing the handler's normalize step) can't
// accidentally persist an empty-string orchestrator_id and bypass the
// schema-level DEFAULT.
func (s *Store) InsertHuddle(ctx context.Context, h types.Huddle) error {
	if h.OrchestratorID == "" {
		h.OrchestratorID = DefaultOrchestratorID
	}
	if h.OrchestratorDisplayName == "" {
		h.OrchestratorDisplayName = DefaultOrchestratorDisplayName
	}

	var closed sql.NullString
	if h.ClosedAt != nil {
		closed = sql.NullString{String: h.ClosedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	var ttl sql.NullInt64
	if h.TTLHours != nil {
		ttl = sql.NullInt64{Int64: int64(*h.TTLHours), Valid: true}
	}

	q := `
INSERT INTO huddles (
  id, purpose, orchestrator_id, orchestrator_display_name,
  slack_channel_id, slack_channel_name, created_at, closed_at, ttl_hours
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q,
		h.ID,
		h.Purpose,
		h.OrchestratorID,
		h.OrchestratorDisplayName,
		h.SlackChannelID,
		h.SlackChannelName,
		h.CreatedAt.UTC().Format(time.RFC3339Nano),
		closed,
		ttl,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	return nil
}

// LookupHuddle returns a huddle by id.
func (s *Store) LookupHuddle(ctx context.Context, id string) (types.Huddle, error) {
	q := `
SELECT id, purpose, orchestrator_id, orchestrator_display_name, slack_channel_id, slack_channel_name,
       created_at, closed_at, ttl_hours
FROM huddles WHERE id = ?`
	row := s.db.QueryRowContext(ctx, q, id)

	var (
		h          types.Huddle
		createdRaw string
		closedRaw  sql.NullString
		ttl        sql.NullInt64
	)

	err := row.Scan(
		&h.ID,
		&h.Purpose,
		&h.OrchestratorID,
		&h.OrchestratorDisplayName,
		&h.SlackChannelID,
		&h.SlackChannelName,
		&createdRaw,
		&closedRaw,
		&ttl,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return types.Huddle{}, huddleerr.ErrHuddleNotFound
	}

	if err != nil {
		return types.Huddle{}, fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	h.CreatedAt, err = time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return types.Huddle{}, fmt.Errorf("%w: parse created_at: %w", huddleerr.ErrStorageFailure, err)
	}

	if closedRaw.Valid {
		t, perr := time.Parse(time.RFC3339Nano, closedRaw.String)
		if perr != nil {
			return types.Huddle{}, fmt.Errorf("%w: parse closed_at: %w", huddleerr.ErrStorageFailure, perr)
		}

		h.ClosedAt = &t
	}

	if ttl.Valid {
		v := int(ttl.Int64)
		h.TTLHours = &v
	}

	return h, nil
}

// ListHuddles returns all huddles, optionally restricted to open rows.
func (s *Store) ListHuddles(ctx context.Context, activeOnly bool) ([]types.Huddle, error) {
	q := `
SELECT id, purpose, orchestrator_id, orchestrator_display_name, slack_channel_id, slack_channel_name,
       created_at, closed_at, ttl_hours
FROM huddles`
	if activeOnly {
		q += ` WHERE closed_at IS NULL`
	}
	q += ` ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}
	defer func() { _ = rows.Close() }()

	var out []types.Huddle

	for rows.Next() {
		var (
			h          types.Huddle
			createdRaw string
			closedRaw  sql.NullString
			ttl        sql.NullInt64
		)

		if scanErr := rows.Scan(
			&h.ID,
			&h.Purpose,
			&h.OrchestratorID,
			&h.OrchestratorDisplayName,
			&h.SlackChannelID,
			&h.SlackChannelName,
			&createdRaw,
			&closedRaw,
			&ttl,
		); scanErr != nil {
			return nil, fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, scanErr)
		}

		h.CreatedAt, err = time.Parse(time.RFC3339Nano, createdRaw)
		if err != nil {
			return nil, fmt.Errorf("%w: parse created_at: %w", huddleerr.ErrStorageFailure, err)
		}

		if closedRaw.Valid {
			t, perr := time.Parse(time.RFC3339Nano, closedRaw.String)
			if perr != nil {
				return nil, fmt.Errorf("%w: parse closed_at: %w", huddleerr.ErrStorageFailure, perr)
			}

			h.ClosedAt = &t
		}

		if ttl.Valid {
			v := int(ttl.Int64)
			h.TTLHours = &v
		}

		out = append(out, h)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	return out, nil
}

// DeleteHuddle removes a huddle row by id; ON DELETE CASCADE on the keys FK
// drops any associated seat keys. Used by huddle.create's compensation path
// when post-channel-creation persistence fails partway through.
func (s *Store) DeleteHuddle(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM huddles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	if n == 0 {
		return huddleerr.ErrHuddleNotFound
	}

	return nil
}

// MarkClosed records closed_at for a huddle id.
func (s *Store) MarkClosed(ctx context.Context, id string, t time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE huddles SET closed_at = ? WHERE id = ?`, t.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	if n == 0 {
		return huddleerr.ErrHuddleNotFound
	}

	return nil
}
