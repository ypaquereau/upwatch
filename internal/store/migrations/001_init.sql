CREATE TABLE targets (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL,
    url         TEXT        NOT NULL UNIQUE,
    interval_s  INTEGER     NOT NULL DEFAULT 30 CHECK (interval_s >= 10),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE checks (
    id           BIGSERIAL PRIMARY KEY,
    target_id    BIGINT      NOT NULL REFERENCES targets(id) ON DELETE CASCADE,
    checked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    status_code  INTEGER,
    latency_ms   INTEGER,
    success      BOOLEAN     NOT NULL,
    error        TEXT
);

CREATE INDEX idx_checks_target_time ON checks (target_id, checked_at DESC);
