import os
import re

os.chdir("engines/shield/internal/cache")

with open("cache.go", "r") as f:
    content = f.read()

# Add singleflight import
content = content.replace('"sync"', '"sync"\n\t"golang.org/x/sync/singleflight"')

# Add request group to struct
content = content.replace('db *sql.DB\n}', 'db *sql.DB\n\tsg singleflight.Group\n}')

get_body = """
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
"""

# Replace Get method body
content = re.sub(r'func \(c \*MaterializedView\) Get\(ctx context\.Context, fingerprint string\) \(blockReason string, isAllowed bool, cacheAge time\.Duration, err error\) \{.*?\n\}', 
                 'func (c *MaterializedView) Get(ctx context.Context, fingerprint string) (blockReason string, isAllowed bool, cacheAge time.Duration, err error) {\n' + get_body + '\n}', 
                 content, flags=re.DOTALL)

with open("cache.go", "w") as f:
    f.write(content)

