ALTER TABLE sessions ADD COLUMN service_tier TEXT NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN service_tier TEXT;
