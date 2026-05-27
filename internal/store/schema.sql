-- The 'orchestrator' default below must agree with store.DefaultOrchestratorID
-- and DefaultOrchestratorDisplayName in huddles.go (and the backfill DDL in
-- db.go) — they form one logical default split across SQL DDL + Go.
CREATE TABLE IF NOT EXISTS huddles (
  id                          TEXT PRIMARY KEY,
  purpose                     TEXT NOT NULL,
  orchestrator_id             TEXT NOT NULL DEFAULT 'orchestrator',
  orchestrator_display_name   TEXT NOT NULL DEFAULT 'orchestrator',
  slack_channel_id            TEXT NOT NULL UNIQUE,
  slack_channel_name          TEXT NOT NULL UNIQUE,
  created_at                  TEXT NOT NULL,
  closed_at                   TEXT,
  ttl_hours                   INTEGER
);

CREATE INDEX IF NOT EXISTS idx_huddles_open ON huddles(closed_at) WHERE closed_at IS NULL;

CREATE TABLE IF NOT EXISTS keys (
  key                         TEXT PRIMARY KEY,
  huddle_id                   TEXT NOT NULL,
  seat_id                     TEXT NOT NULL,
  display_name                TEXT NOT NULL,
  created_at                  TEXT NOT NULL,
  revoked_at                  TEXT,
  FOREIGN KEY (huddle_id) REFERENCES huddles(id) ON DELETE CASCADE,
  UNIQUE (huddle_id, seat_id)
);

CREATE INDEX IF NOT EXISTS idx_keys_huddle  ON keys(huddle_id);
CREATE INDEX IF NOT EXISTS idx_keys_active  ON keys(key) WHERE revoked_at IS NULL;
