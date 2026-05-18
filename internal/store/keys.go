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

// Key is a persisted seat key row (table name: keys).
type Key struct {
	Key         string
	HuddleID    string
	SeatID      string
	DisplayName string
	CreatedAt   time.Time
	RevokedAt   *time.Time
}

// InsertKey stores a new active key for a seat.
func (s *Store) InsertKey(ctx context.Context, k Key) error {
	var revoked sql.NullString
	if k.RevokedAt != nil {
		revoked = sql.NullString{String: k.RevokedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	q := `
INSERT INTO keys (key, huddle_id, seat_id, display_name, created_at, revoked_at)
VALUES (?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q,
		k.Key,
		k.HuddleID,
		k.SeatID,
		k.DisplayName,
		k.CreatedAt.UTC().Format(time.RFC3339Nano),
		revoked,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	return nil
}

// LookupKey resolves a key value; revoked or missing keys surface [huddleerr.ErrKeyInvalid].
func (s *Store) LookupKey(ctx context.Context, key string) (Key, error) {
	q := `
SELECT key, huddle_id, seat_id, display_name, created_at, revoked_at
FROM keys WHERE key = ?`
	row := s.db.QueryRowContext(ctx, q, key)

	var (
		k          Key
		createdRaw string
		revokedRaw sql.NullString
	)

	err := row.Scan(
		&k.Key,
		&k.HuddleID,
		&k.SeatID,
		&k.DisplayName,
		&createdRaw,
		&revokedRaw,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return Key{}, huddleerr.ErrKeyInvalid
	}

	if err != nil {
		return Key{}, fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	k.CreatedAt, err = time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return Key{}, fmt.Errorf("%w: parse created_at: %w", huddleerr.ErrStorageFailure, err)
	}

	// A non-NULL revoked_at means the key is invalid; surface that semantic
	// directly. Don't bother parsing the timestamp — callers receive a zero Key
	// and ErrKeyInvalid either way, and parsing would risk masking the semantic
	// behind an ErrStorageFailure if the stored value happens to be malformed.
	if revokedRaw.Valid {
		return Key{}, huddleerr.ErrKeyInvalid
	}

	return k, nil
}

// ListSeats returns seats derived from keys that remain active for a huddle.
func (s *Store) ListSeats(ctx context.Context, huddleID string) ([]types.Seat, error) {
	q := `
SELECT seat_id, display_name
FROM keys
WHERE huddle_id = ? AND revoked_at IS NULL
ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, huddleID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}
	defer func() { _ = rows.Close() }()

	var seats []types.Seat

	for rows.Next() {
		var seat types.Seat
		if scanErr := rows.Scan(&seat.ID, &seat.DisplayName); scanErr != nil {
			return nil, fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, scanErr)
		}

		seats = append(seats, seat)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	return seats, nil
}

// RevokeKey stamps revoked_at (UTC) onto a matching key row.
func (s *Store) RevokeKey(ctx context.Context, key string, revoked time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE keys SET revoked_at = ? WHERE key = ?`,
		revoked.UTC().Format(time.RFC3339Nano),
		key,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	if n == 0 {
		return huddleerr.ErrKeyInvalid
	}

	return nil
}
