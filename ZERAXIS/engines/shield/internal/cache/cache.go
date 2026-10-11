package cache

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"golang.org/x/sync/singleflight"
	"time"
)

type Entry struct {
	BlockReason string
	IsAllowed   bool
	ExpiresAt   time.Time
	CachedAt    time.Time
}

type MaterializedView struct {
	mu sync.RWMutex
	m  map[string]*Entry
	db *sql.DB
	sg singleflight.Group
}

func NewMaterializedView(db *sql.DB) *MaterializedView {
	return &MaterializedView{
		m:  make(map[string]*Entry),
		db: db,
	}
}

func (c *MaterializedView) LoadFromDB(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	rows, err := c.db.QueryContext(ctx, "SELECT fingerprint, block_reason, cached_at, ttl_expires_at FROM shield_cache_state")
	if err != nil {
		return fmt.Errorf("failed to load cache state: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var fp string
		var reason sql.NullString
		var cachedAt, ttl time.Time
		if err := rows.Scan(&fp, &reason, &cachedAt, &ttl); err != nil {
			return err
		}
		
		isAllowed := true
		blockReason := ""
		if reason.Valid {
			isAllowed = false
			blockReason = reason.String
		}

		c.m[fp] = &Entry{
			BlockReason: blockReason,
			IsAllowed:   isAllowed,
			CachedAt:    cachedAt,
			ExpiresAt:   ttl,
		}
	}
	return rows.Err()
}

func (c *MaterializedView) Get(ctx context.Context, fingerprint string) (blockReason string, isAllowed bool, cacheAge time.Duration, err error) {

	c.mu.RLock()
	entry, ok := c.m[fingerprint]
	c.mu.RUnlock()

	now := time.Now()
	if ok && now.Before(entry.ExpiresAt) {
		return entry.BlockReason, entry.IsAllowed, now.Sub(entry.CachedAt), nil
	}

	// Use singleflight to prevent DB stampede on cache miss
	type result struct {
		reason  string
		allowed bool
		age     time.Duration
	}

	res, err, _ := c.sg.Do(fingerprint, func() (interface{}, error) {
		var reason sql.NullString
		var cachedAt time.Time
		err := c.db.QueryRowContext(ctx, "SELECT block_reason, cached_at FROM shield_cache_state WHERE fingerprint = $1", fingerprint).Scan(&reason, &cachedAt)
		if err != nil {
			if err == sql.ErrNoRows {
				c.Set(ctx, fingerprint, "", 5*time.Minute)
				return result{"", true, 0}, nil
			}
			return nil, fmt.Errorf("fallback query failed: %w", err)
		}

		isAllowed := true
		blockReason := ""
		if reason.Valid && reason.String != "" {
			isAllowed = false
			blockReason = reason.String
		}

		c.Set(ctx, fingerprint, blockReason, 5*time.Minute)
		return result{blockReason, isAllowed, time.Since(cachedAt)}, nil
	})

	if err != nil {
		return "DB_UNREACHABLE", false, 0, err
	}

	r := res.(result)
	return r.reason, r.allowed, r.age, nil

}

func (c *MaterializedView) Set(ctx context.Context, fingerprint, blockReason string, ttl time.Duration) error {
	now := time.Now()
	expiresAt := now.Add(ttl)

	c.mu.Lock()
	c.m[fingerprint] = &Entry{
		BlockReason: blockReason,
		IsAllowed:   blockReason == "",
		CachedAt:    now,
		ExpiresAt:   expiresAt,
	}
	c.mu.Unlock()

	var reason sql.NullString
	if blockReason != "" {
		reason = sql.NullString{String: blockReason, Valid: true}
	}

	_, err := c.db.ExecContext(ctx, `
		INSERT INTO shield_cache_state (fingerprint, block_reason, cached_at, ttl_expires_at) 
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (fingerprint) DO UPDATE SET 
			block_reason = EXCLUDED.block_reason,
			cached_at = EXCLUDED.cached_at,
			ttl_expires_at = EXCLUDED.ttl_expires_at
	`, fingerprint, reason, now, expiresAt)
	
	if err != nil {
		// Log error but memory is updated
		return err
	}
	return nil
}

func (c *MaterializedView) Invalidate(ctx context.Context, fingerprint string) error {
	c.mu.Lock()
	delete(c.m, fingerprint)
	c.mu.Unlock()

	_, err := c.db.ExecContext(ctx, "DELETE FROM shield_cache_state WHERE fingerprint = $1", fingerprint)
	return err
}

func (c *MaterializedView) StartCleanupJob(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.mu.Lock()
				now := time.Now()
				for fp, entry := range c.m {
					if now.After(entry.ExpiresAt) {
						delete(c.m, fp)
					}
				}
				c.mu.Unlock()
				// DB cleanup
				c.db.ExecContext(context.Background(), "DELETE FROM shield_cache_state WHERE ttl_expires_at <= $1", now)
			}
		}
	}()
}
