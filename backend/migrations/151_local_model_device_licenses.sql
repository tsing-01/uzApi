ALTER TABLE users ADD COLUMN IF NOT EXISTS local_model_access_revoked_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS local_model_access_version BIGINT NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS local_model_devices (
    id VARCHAR(32) PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    public_key JSONB NOT NULL,
    key_thumbprint VARCHAR(43) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_issued_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    UNIQUE (user_id, key_thumbprint)
);
CREATE INDEX IF NOT EXISTS idx_local_model_devices_user ON local_model_devices(user_id);

CREATE TABLE IF NOT EXISTS local_model_challenges (
    token_hash VARCHAR(64) PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action VARCHAR(16) NOT NULL CHECK (action IN ('register', 'issue', 'renew')),
    device_id VARCHAR(32) REFERENCES local_model_devices(id) ON DELETE CASCADE,
    public_key JSONB NOT NULL,
    key_thumbprint VARCHAR(43) NOT NULL,
    grant_version BIGINT NOT NULL,
    token_version BIGINT NOT NULL,
    previous_license_hash VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_local_model_challenges_user_time ON local_model_challenges(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_local_model_challenges_expiry ON local_model_challenges(expires_at);

CREATE TABLE IF NOT EXISTS local_model_license_events (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id VARCHAR(32),
    actor_id BIGINT NOT NULL,
    event VARCHAR(32) NOT NULL,
    reason VARCHAR(500) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_local_model_license_events_user_time ON local_model_license_events(user_id, created_at);
