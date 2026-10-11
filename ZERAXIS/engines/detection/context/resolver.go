package context

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"
	"container/list"
)

type Package struct {
	Name    string
	Version string
	PURL    string
	Paths   []string
}

type Vulnerability struct {
	VulnID           string
	Identifier       string
	Severity         string
	PackageName      string
	InstalledVersion string
	FixedVersion     string
}

type ImageContext struct {
	ImageDigest     string
	Packages        []Package
	Vulnerabilities []Vulnerability
	PolicyResult    string
	PolicyCritical  int
	Unknown         bool
	FetchedAt       time.Time
}

type cacheEntry struct {
	key string
	val ImageContext
}

type Resolver struct {
	db    *sql.DB
	cache map[string]*list.Element
	ll    *list.List
	mu    sync.RWMutex
	max   int
}

func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{
		db:    db,
		cache: make(map[string]*list.Element),
		ll:    list.New(),
		max:   1000,
	}
}

func (r *Resolver) GetContext(ctx context.Context, imageDigest string) ImageContext {
	r.mu.Lock()
	if ele, hit := r.cache[imageDigest]; hit {
		r.ll.MoveToFront(ele)
		val := ele.Value.(*cacheEntry).val
		r.mu.Unlock()

		ttl := 1 * time.Hour
		if val.Unknown {
			ttl = 5 * time.Minute
		}
		if time.Since(val.FetchedAt) < ttl {
			return val
		}
	} else {
		r.mu.Unlock()
	}

	// Fetch from DB
	imgCtx := r.fetchFromDB(ctx, imageDigest)
	imgCtx.FetchedAt = time.Now()

	r.mu.Lock()
	if ele, hit := r.cache[imageDigest]; hit {
		ele.Value.(*cacheEntry).val = imgCtx
		r.ll.MoveToFront(ele)
	} else {
		ele := r.ll.PushFront(&cacheEntry{key: imageDigest, val: imgCtx})
		r.cache[imageDigest] = ele
		if r.ll.Len() > r.max {
			oldest := r.ll.Back()
			if oldest != nil {
				r.ll.Remove(oldest)
				delete(r.cache, oldest.Value.(*cacheEntry).key)
			}
		}
	}
	r.mu.Unlock()

	return imgCtx
}

func (r *Resolver) fetchFromDB(ctx context.Context, digest string) ImageContext {
	res := ImageContext{ImageDigest: digest, Unknown: true}

	var imageID string
	err := r.db.QueryRowContext(ctx, "SELECT image_id FROM supply_chain_images WHERE digest = $1", digest).Scan(&imageID)
	if err != nil {
		return res
	}

	_ = r.db.QueryRowContext(ctx, "SELECT result, critical_count FROM supply_chain_policy_decisions WHERE image_id = $1 ORDER BY evaluated_at DESC LIMIT 1", imageID).Scan(&res.PolicyResult, &res.PolicyCritical)

	var rawSbomB []byte
	var sbomID string
	err = r.db.QueryRowContext(ctx, "SELECT sbom_id, raw_sbom FROM supply_chain_sboms WHERE image_id = $1 ORDER BY generated_at DESC LIMIT 1", imageID).Scan(&sbomID, &rawSbomB)
	
	pkgPaths := make(map[string][]string)
	if err == nil && len(rawSbomB) > 0 {
		var rawSbom map[string]interface{}
		if json.Unmarshal(rawSbomB, &rawSbom) == nil {
			if components, ok := rawSbom["components"].([]interface{}); ok {
				for _, c := range components {
					comp := c.(map[string]interface{})
					name, _ := comp["name"].(string)
					if props, ok := comp["properties"].([]interface{}); ok {
						for _, p := range props {
							prop := p.(map[string]interface{})
							if prop["name"] == "syft:location:0:path" {
								if path, ok := prop["value"].(string); ok {
									pkgPaths[name] = append(pkgPaths[name], path)
								}
							}
						}
					}
				}
			}
		}
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT v.vuln_id, v.vulnerability_identifier, v.severity, v.installed_version, v.fixed_version, p.name 
		FROM supply_chain_vulnerabilities v
		JOIN supply_chain_packages p ON v.package_id = p.package_id
		WHERE v.image_id = $1
	`, imageID)
	if err != nil {
		return res
	}
	defer rows.Close()

	for rows.Next() {
		var v Vulnerability
		err := rows.Scan(&v.VulnID, &v.Identifier, &v.Severity, &v.InstalledVersion, &v.FixedVersion, &v.PackageName)
		if err == nil {
			res.Vulnerabilities = append(res.Vulnerabilities, v)
			found := false
			for _, pkg := range res.Packages {
				if pkg.Name == v.PackageName {
					found = true
					break
				}
			}
			if !found {
				res.Packages = append(res.Packages, Package{
					Name: v.PackageName,
					Version: v.InstalledVersion,
					Paths: pkgPaths[v.PackageName],
				})
			}
		}
	}

	res.Unknown = false
	return res
}
