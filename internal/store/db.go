// Package store holds SQLite-backed persistence for huddles and seat keys.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	huddleerr "github.com/itsHabib/huddle/internal/errors"

	// Pure-Go SQLite (no CGO).
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps SQLite access with small domain helpers.
type Store struct {
	db       *sql.DB
	stateDir string // resolved absolute state directory
	dbPath   string // resolved absolute path to huddle.sqlite
	freshDB  bool   // New created a brand-new empty database file
}

// sqliteDSN converts a filesystem path into a SQLite DSN understood by modernc.org/sqlite.
func sqliteDSN(absPath string) string {
	sp := filepath.ToSlash(absPath)

	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", sp)
}

// New opens SQLite under stateDir/huddle.sqlite, ensuring the schema exists.
func New(stateDir string) (*Store, error) {
	stateDir = filepath.Clean(stateDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	dbPath := filepath.Join(stateDir, "huddle.sqlite")
	absPath, absErr := filepath.Abs(dbPath)
	if absErr != nil {
		return nil, absErr
	}

	absStateDir, absDirErr := filepath.Abs(stateDir)
	if absDirErr != nil {
		return nil, absDirErr
	}

	// MkdirAll above created only the directory, not the DB file, so an absent
	// file here means we're about to create a brand-new database. A fresh DB
	// alongside zero huddles is the classic sign HUDDLE_STATE_DIR is wrong.
	_, statErr := os.Stat(absPath)
	freshDB := os.IsNotExist(statErr)

	db, err := sql.Open("sqlite", sqliteDSN(absPath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db, stateDir: absStateDir, dbPath: absPath, freshDB: freshDB}
	if err = s.ApplySchema(context.Background()); err != nil {
		closeErr := s.Close()

		return nil, errors.Join(err, closeErr)
	}

	return s, nil
}

// OpenMemory opens an isolated in-memory SQLite (for tests). Each call returns
// a fresh, independent DB — the DSN is uniquified per call so parallel tests
// don't see each other's rows.
func OpenMemory(ctx context.Context) (*Store, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return nil, fmt.Errorf("generate memory db name: %w", err)
	}
	name := hex.EncodeToString(buf[:])

	dsn := fmt.Sprintf("file:huddle-mem-%s?mode=memory&cache=shared&_pragma=foreign_keys(ON)", name)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.ApplySchema(ctx); err != nil {
		closeErr := s.Close()

		return nil, errors.Join(err, closeErr)
	}

	return s, nil
}

// ApplySchema runs the embedded DDL idempotently.
func (s *Store) ApplySchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: nil connection", huddleerr.ErrStorageFailure)
	}

	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("%w: %w", huddleerr.ErrStorageFailure, err)
	}

	if err := s.backfillColumns(ctx); err != nil {
		return err
	}

	return nil
}

// backfillColumns adds columns that landed after the original CREATE TABLE
// to pre-existing databases. SQLite has no `ALTER TABLE ... ADD COLUMN IF
// NOT EXISTS`, so we probe pragma_table_info per column and add only when
// missing. New databases already have these columns via CREATE TABLE.
func (s *Store) backfillColumns(ctx context.Context) error {
	cols := []struct{ table, column, ddl string }{
		{"huddles", "orchestrator_id", fmt.Sprintf(`ALTER TABLE huddles ADD COLUMN orchestrator_id TEXT NOT NULL DEFAULT '%s'`, DefaultOrchestratorID)},
	}

	for _, c := range cols {
		var present int
		row := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`,
			c.table, c.column)
		if err := row.Scan(&present); err != nil {
			return fmt.Errorf("%w: probe %s.%s: %w", huddleerr.ErrStorageFailure, c.table, c.column, err)
		}

		if present > 0 {
			continue
		}

		if _, err := s.db.ExecContext(ctx, c.ddl); err != nil {
			return fmt.Errorf("%w: backfill %s.%s: %w", huddleerr.ErrStorageFailure, c.table, c.column, err)
		}
	}

	return nil
}

// Close shuts down database handles.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	err := s.db.Close()
	s.db = nil

	return err
}

// StateDir returns the resolved absolute state directory backing this store.
func (s *Store) StateDir() string {
	return s.stateDir
}

// DBPath returns the resolved absolute path to the SQLite database file.
func (s *Store) DBPath() string {
	return s.dbPath
}

// CreatedFreshDB reports whether New created a brand-new empty database (no
// huddle.sqlite existed at the resolved path). A true value together with zero
// existing huddles is the classic sign HUDDLE_STATE_DIR points at the wrong dir.
func (s *Store) CreatedFreshDB() bool {
	return s.freshDB
}
