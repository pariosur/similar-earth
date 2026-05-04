package db

import (
	"context"
	"fmt"
)

// Migrate runs CREATE TABLE IF NOT EXISTS for all required tables and indexes.
func (d *DB) Migrate(ctx context.Context) error {
	ddl := `
		CREATE TABLE IF NOT EXISTS queries (
			id          UUID PRIMARY KEY,
			pins        JSONB NOT NULL,
			pin_count   INT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'pending',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			completed_at TIMESTAMPTZ,
			compute_ms  INT,
			error       TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_queries_created_at
			ON queries (created_at DESC);

		CREATE TABLE IF NOT EXISTS event_logs (
			id          BIGSERIAL PRIMARY KEY,
			event_type  TEXT NOT NULL,
			query_id    UUID,
			payload     JSONB,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE INDEX IF NOT EXISTS idx_event_logs_event_type
			ON event_logs (event_type);

		CREATE INDEX IF NOT EXISTS idx_event_logs_created_at
			ON event_logs (created_at DESC);

		CREATE TABLE IF NOT EXISTS maps (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			slug        TEXT NOT NULL DEFAULT '',
			title       TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			category    TEXT NOT NULL DEFAULT '',
			pins        JSONB NOT NULL,
			pin_count   INT NOT NULL,
			author      TEXT NOT NULL DEFAULT 'anonymous',
			stars       INT NOT NULL DEFAULT 0,
			views       INT NOT NULL DEFAULT 0,
			is_featured BOOLEAN NOT NULL DEFAULT false,
			has_tiles   BOOLEAN NOT NULL DEFAULT false,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		-- Add slug column if missing (for existing DBs)
		DO $$ BEGIN
			ALTER TABLE maps ADD COLUMN IF NOT EXISTS slug TEXT NOT NULL DEFAULT '';
		EXCEPTION WHEN others THEN NULL;
		END $$;

		-- Unique slug for featured map upsert (excludes empty slugs)
		CREATE UNIQUE INDEX IF NOT EXISTS idx_maps_slug_unique
			ON maps (slug) WHERE slug != '';

		CREATE INDEX IF NOT EXISTS idx_maps_stars
			ON maps (stars DESC);

		CREATE INDEX IF NOT EXISTS idx_maps_views
			ON maps (views DESC);

		CREATE INDEX IF NOT EXISTS idx_maps_category
			ON maps (category);

		CREATE INDEX IF NOT EXISTS idx_maps_featured
			ON maps (is_featured) WHERE is_featured = true;

		CREATE INDEX IF NOT EXISTS idx_maps_created_at
			ON maps (created_at DESC);
	`
	if _, err := d.Pool.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
