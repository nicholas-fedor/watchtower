-- Watchtower e2e control-plane schema.

CREATE TABLE IF NOT EXISTS runs (
    id            TEXT PRIMARY KEY,
    label         TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL,
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    status        TEXT NOT NULL,
    generator     TEXT NOT NULL,
    seed          BIGINT NOT NULL DEFAULT 1,
    topic         TEXT NOT NULL DEFAULT '',
    filter        TEXT NOT NULL DEFAULT '',
    file_path     TEXT NOT NULL DEFAULT '',
    shard         TEXT NOT NULL DEFAULT '',
    offset_n      INTEGER NOT NULL DEFAULT 0,
    limit_n       INTEGER NOT NULL DEFAULT 0,
    workers       INTEGER NOT NULL DEFAULT 0,
    keep          BOOLEAN NOT NULL DEFAULT FALSE,
    passed        INTEGER NOT NULL DEFAULT 0,
    failed        INTEGER NOT NULL DEFAULT 0,
    skipped       INTEGER NOT NULL DEFAULT 0,
    error         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS runs_status_created ON runs (status, created_at DESC);

-- At most one sitting occupies the execution slot.
CREATE UNIQUE INDEX IF NOT EXISTS runs_one_running ON runs (status) WHERE status = 'running';

CREATE TABLE IF NOT EXISTS cases (
    run_id          TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    case_id         TEXT NOT NULL,
    status          TEXT NOT NULL,
    factors         JSONB NOT NULL DEFAULT '{}'::jsonb,
    expect          JSONB,
    argv            JSONB,
    env             JSONB,
    error           TEXT NOT NULL DEFAULT '',
    duration_ms     BIGINT NOT NULL DEFAULT 0,
    inspect_before  JSONB,
    inspect_after   JSONB,
    porcelain       JSONB,
    http_details    TEXT NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    PRIMARY KEY (run_id, case_id)
);

CREATE INDEX IF NOT EXISTS cases_run_status ON cases (run_id, status);

CREATE TABLE IF NOT EXISTS events (
    id          BIGSERIAL PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    case_id     TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS events_run_id ON events (run_id, id);
