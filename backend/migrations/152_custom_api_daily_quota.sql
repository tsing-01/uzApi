-- Authoritative account-wide quota. Neither table depends on Redis or client clocks.
CREATE TABLE IF NOT EXISTS local_model_daily_usage (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    quota_date DATE NOT NULL,
    used BIGINT NOT NULL DEFAULT 0 CHECK (used >= 0),
    rate_window TIMESTAMPTZ NOT NULL,
    rate_count INTEGER NOT NULL DEFAULT 0 CHECK (rate_count >= 0),
    PRIMARY KEY (user_id, quota_date)
);

CREATE TABLE IF NOT EXISTS local_model_request_decisions (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_id UUID NOT NULL,
    quota_date DATE NOT NULL,
    allowed BOOLEAN NOT NULL,
    entitlement_enabled BOOLEAN NOT NULL,
    entitlement_version BIGINT NOT NULL,
    used BIGINT NOT NULL CHECK (used >= 0),
    decided_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, request_id),
    CHECK (expires_at > decided_at)
);
CREATE INDEX IF NOT EXISTS idx_local_model_decisions_user_time
    ON local_model_request_decisions(user_id, decided_at);

-- Reuse the existing audit stream; old device events may leave these fields NULL.
ALTER TABLE local_model_license_events
    ADD COLUMN IF NOT EXISTS previous_enabled BOOLEAN,
    ADD COLUMN IF NOT EXISTS new_enabled BOOLEAN,
    ADD COLUMN IF NOT EXISTS previous_version BIGINT,
    ADD COLUMN IF NOT EXISTS new_version BIGINT,
    ADD COLUMN IF NOT EXISTS redeem_code_id BIGINT;
