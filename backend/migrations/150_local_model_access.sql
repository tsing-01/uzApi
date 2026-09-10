-- NULL means the user has not redeemed the paid local-model entitlement.
-- Redemption sets this once, atomically with consuming a local_model_access code.
ALTER TABLE users ADD COLUMN IF NOT EXISTS local_model_access_unlocked_at TIMESTAMPTZ;
