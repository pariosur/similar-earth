package tiles

import "sync"

// TileCache is a simple in-memory LRU cache for rendered PNG tiles.
type TileCache struct {
	mu      sync.Mutex
	entries map[string][]byte // key = "queryID/z/x/y"
	order   []string          // insertion order, oldest first
	maxSize int               // max number of cached entries
}

// NewTileCache creates a cache that holds up to maxEntries tiles.
func NewTileCache(maxEntries int) *TileCache {
	return &TileCache{
		entries: make(map[string][]byte),
		maxSize: maxEntries,
	}
}

// Get retrieves a cached tile. Returns the PNG bytes and true on hit,
// or nil and false on miss.
func (c *TileCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.entries[key]
	if ok {
		// Move to end (most recently used).
		c.moveToEnd(key)
	}
	return data, ok
}

// Put stores a rendered tile in the cache, evicting the oldest entry
// if the cache is full.
func (c *TileCache) Put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; exists {
		c.entries[key] = data
		c.moveToEnd(key)
		return
	}

	// Evict oldest if at capacity.
	for len(c.entries) >= c.maxSize && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}

	c.entries[key] = data
	c.order = append(c.order, key)
}

// moveToEnd removes key from its current position in order and appends it.
func (c *TileCache) moveToEnd(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}
