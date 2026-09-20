-- +goose Up
ALTER TABLE conversations ADD COLUMN service_tier TEXT;
ALTER TABLE sessions ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN service_tier TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN service_tier;
ALTER TABLE sessions DROP COLUMN reasoning_effort;
ALTER TABLE conversations DROP COLUMN service_tier;
