ALTER TABLE items
ADD COLUMN IF NOT EXISTS bullet_total_count integer NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS bullet_included_count integer NOT NULL DEFAULT 0;
