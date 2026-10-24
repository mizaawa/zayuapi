//go:build embed

package web

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// htmlCacheTTL bounds propagation of settings changed by another application
// instance sharing the database. Even an unrestricted UI snapshot expires.
// Refresh is lazy on the next document request; no background poll is needed.
const htmlCacheTTL = 5 * time.Second

// HTMLCache manages the cached index.html with injected settings
type HTMLCache struct {
	mu              sync.RWMutex
	cachedHTML      []byte
	cachedAt        time.Time
	accessPolicy    uiAccessPolicy
	etag            string
	baseHTMLHash    string // Hash of the original index.html (immutable after build)
	settingsVersion uint64 // Incremented when settings change
}

// CachedHTML represents the cache state
type CachedHTML struct {
	Content        []byte
	ETag           string
	UIAccessDomain string
	AccessPolicy   uiAccessPolicy
}

// NewHTMLCache creates a new HTML cache instance
func NewHTMLCache() *HTMLCache {
	return &HTMLCache{}
}

// SetBaseHTML initializes the cache with the base HTML template
func (c *HTMLCache) SetBaseHTML(baseHTML []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hash := sha256.Sum256(baseHTML)
	c.baseHTMLHash = hex.EncodeToString(hash[:8]) // First 8 bytes for brevity
}

// Invalidate marks the cache as stale
func (c *HTMLCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.settingsVersion++
	c.cachedHTML = nil
	c.cachedAt = time.Time{}
	c.etag = ""
	c.accessPolicy = uiAccessPolicy{}
}

// Get returns the cached HTML or nil if cache is stale
func (c *HTMLCache) Get() *CachedHTML {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cachedHTML == nil || time.Since(c.cachedAt) >= htmlCacheTTL {
		return nil
	}
	return &CachedHTML{
		Content:        c.cachedHTML,
		ETag:           c.etag,
		UIAccessDomain: c.accessPolicy.Domain,
		AccessPolicy:   c.accessPolicy,
	}
}

// Set updates the cache with new rendered HTML
func (c *HTMLCache) Set(html []byte, settingsJSON []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setLocked(html, settingsJSON)
}

// Generation identifies the settings snapshot before an asynchronous read.
func (c *HTMLCache) Generation() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settingsVersion
}

// SetIfGeneration prevents an in-flight old read from undoing invalidation.
func (c *HTMLCache) SetIfGeneration(html, settingsJSON []byte, generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.settingsVersion {
		return false
	}
	c.setLocked(html, settingsJSON)
	return true
}

func (c *HTMLCache) setLocked(html []byte, settingsJSON []byte) {
	settings, _ := parseUIAccessPolicy(settingsJSON)
	c.cachedHTML = html
	c.cachedAt = time.Now()
	c.accessPolicy = settings
	c.etag = c.generateETag(settingsJSON)
}

// generateETag creates an ETag from base HTML hash + settings hash
func (c *HTMLCache) generateETag(settingsJSON []byte) string {
	settingsHash := sha256.Sum256(settingsJSON)
	return `"` + c.baseHTMLHash + "-" + hex.EncodeToString(settingsHash[:8]) + `"`
}
