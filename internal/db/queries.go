package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Query represents a row in the queries table.
type Query struct {
	ID          uuid.UUID
	Pins        json.RawMessage
	PinCount    int
	Status      string
	CreatedAt   time.Time
	CompletedAt *time.Time
	ComputeMs   *int
}

// CreateQuery inserts a new query record with status "pending".
func (d *DB) CreateQuery(ctx context.Context, id uuid.UUID, pins json.RawMessage, pinCount int) error {
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO queries (id, pins, pin_count, status) VALUES ($1, $2, $3, 'pending')`,
		id, pins, pinCount,
	)
	if err != nil {
		return fmt.Errorf("create query: %w", err)
	}
	return nil
}

// CompleteQuery marks a query as completed with the given compute time.
func (d *DB) CompleteQuery(ctx context.Context, id uuid.UUID, computeMs int) error {
	_, err := d.Pool.Exec(ctx,
		`UPDATE queries SET status = 'completed', completed_at = now(), compute_ms = $2 WHERE id = $1`,
		id, computeMs,
	)
	if err != nil {
		return fmt.Errorf("complete query: %w", err)
	}
	return nil
}

// FailQuery marks a query as failed with an error message.
func (d *DB) FailQuery(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := d.Pool.Exec(ctx,
		`UPDATE queries SET status = 'failed', completed_at = now(), error = $2 WHERE id = $1`,
		id, errMsg,
	)
	if err != nil {
		return fmt.Errorf("fail query: %w", err)
	}
	return nil
}

// GetQuery retrieves a query by ID.
func (d *DB) GetQuery(ctx context.Context, id uuid.UUID) (*Query, error) {
	q := &Query{}
	err := d.Pool.QueryRow(ctx,
		`SELECT id, pins, pin_count, status, created_at, completed_at, compute_ms FROM queries WHERE id = $1`,
		id,
	).Scan(&q.ID, &q.Pins, &q.PinCount, &q.Status, &q.CreatedAt, &q.CompletedAt, &q.ComputeMs)
	if err != nil {
		return nil, fmt.Errorf("get query: %w", err)
	}
	return q, nil
}

// ============================================================================
// Maps
// ============================================================================

// Map represents a published similarity map.
type Map struct {
	ID          uuid.UUID       `json:"id"`
	Slug        string          `json:"slug"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Pins        json.RawMessage `json:"pins"`
	PinCount    int             `json:"pin_count"`
	Author      string          `json:"author"`
	Stars       int             `json:"stars"`
	Views       int             `json:"views"`
	IsFeatured  bool            `json:"is_featured"`
	HasTiles    bool            `json:"has_tiles"`
	CreatedAt   time.Time       `json:"created_at"`
}

// CreateMap inserts a new published map.
func (d *DB) CreateMap(ctx context.Context, m *Map) error {
	err := d.Pool.QueryRow(ctx,
		`INSERT INTO maps (title, description, category, pins, pin_count, author)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		m.Title, m.Description, m.Category, m.Pins, m.PinCount, m.Author,
	).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return fmt.Errorf("create map: %w", err)
	}
	return nil
}

// GetMap retrieves a map by ID and increments the view count.
func (d *DB) GetMap(ctx context.Context, id uuid.UUID) (*Map, error) {
	m := &Map{}
	err := d.Pool.QueryRow(ctx,
		`UPDATE maps SET views = views + 1 WHERE id = $1
		 RETURNING id, slug, title, description, category, pins, pin_count, author, stars, views, is_featured, has_tiles, created_at`,
		id,
	).Scan(&m.ID, &m.Slug, &m.Title, &m.Description, &m.Category, &m.Pins, &m.PinCount,
		&m.Author, &m.Stars, &m.Views, &m.IsFeatured, &m.HasTiles, &m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get map: %w", err)
	}
	return m, nil
}

// ListMaps returns published maps with sorting and pagination.
func (d *DB) ListMaps(ctx context.Context, sortBy string, limit, offset int) ([]*Map, error) {
	orderClause := "created_at DESC"
	switch sortBy {
	case "stars":
		orderClause = "stars DESC, created_at DESC"
	case "views":
		orderClause = "views DESC, created_at DESC"
	case "newest":
		orderClause = "created_at DESC"
	}

	rows, err := d.Pool.Query(ctx,
		fmt.Sprintf(`SELECT id, slug, title, description, category, pins, pin_count, author, stars, views, is_featured, has_tiles, created_at
		 FROM maps ORDER BY is_featured DESC, %s LIMIT $1 OFFSET $2`, orderClause),
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list maps: %w", err)
	}
	defer rows.Close()

	var maps []*Map
	for rows.Next() {
		m := &Map{}
		if err := rows.Scan(&m.ID, &m.Slug, &m.Title, &m.Description, &m.Category, &m.Pins, &m.PinCount,
			&m.Author, &m.Stars, &m.Views, &m.IsFeatured, &m.HasTiles, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan map: %w", err)
		}
		maps = append(maps, m)
	}
	return maps, nil
}

// StarMap increments the star count for a map.
func (d *DB) StarMap(ctx context.Context, id uuid.UUID) error {
	_, err := d.Pool.Exec(ctx, `UPDATE maps SET stars = stars + 1 WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("star map: %w", err)
	}
	return nil
}

// SeedMap inserts a map if it doesn't already exist (by title). Used for featured presets.
func (d *DB) SeedMap(ctx context.Context, m *Map) error {
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO maps (id, slug, title, description, category, pins, pin_count, author, is_featured, has_tiles)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (slug) DO UPDATE SET
		   id = EXCLUDED.id,
		   title = EXCLUDED.title,
		   description = EXCLUDED.description,
		   category = EXCLUDED.category,
		   pins = EXCLUDED.pins,
		   pin_count = EXCLUDED.pin_count,
		   has_tiles = EXCLUDED.has_tiles`,
		m.ID, m.Slug, m.Title, m.Description, m.Category, m.Pins, m.PinCount, m.Author, m.IsFeatured, m.HasTiles,
	)
	if err != nil {
		return fmt.Errorf("seed map: %w", err)
	}
	return nil
}

// LogEvent inserts a row into the event_logs table.
func (d *DB) LogEvent(ctx context.Context, eventType string, queryID *uuid.UUID, payload json.RawMessage) error {
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO event_logs (event_type, query_id, payload) VALUES ($1, $2, $3)`,
		eventType, queryID, payload,
	)
	if err != nil {
		return fmt.Errorf("log event: %w", err)
	}
	return nil
}
