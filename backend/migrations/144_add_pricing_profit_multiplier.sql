INSERT INTO settings (key, value, updated_at)
VALUES ('pricing_profit_multiplier', '1', NOW())
ON CONFLICT (key) DO NOTHING;
