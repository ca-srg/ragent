package mcpserver

import (
	"container/list"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// Constants for cache configuration
const (
	defaultCacheSize = 1000
	defaultCacheTTL  = 5 * time.Minute
)

// BypassIPChecker defines the interface for checking if an IP should bypass authentication
type BypassIPChecker interface {
	ShouldBypass(ipStr string) bool
	AddBypassRange(cidr string) error
	RemoveBypassRange(cidr string) error
	GetBypassRanges() []string
	SetCacheEnabled(enabled bool)
	ClearCache()
}

// cacheEntry represents a cached IP bypass result
type cacheEntry struct {
	bypass    bool
	timestamp time.Time
}

// lruCache implements a simple LRU cache for bypass results
type lruCache struct {
	mu      sync.RWMutex
	maxSize int
	ttl     time.Duration
	cache   map[string]*list.Element
	lruList *list.List
	enabled bool
}

// lruCacheItem represents an item in the LRU list
type lruCacheItem struct {
	key   string
	value cacheEntry
}

// BypassIPCheckerImpl implements the BypassIPChecker interface
type BypassIPCheckerImpl struct {
	mu           sync.RWMutex
	bypassNets   []*net.IPNet
	bypassRanges []string // Store original CIDR strings
	verboseLog   bool
	cache        *lruCache
}

// NewBypassIPChecker creates a new BypassIPChecker instance
func NewBypassIPChecker(bypassRanges []string, verboseLog bool) (*BypassIPCheckerImpl, error) {
	checker := &BypassIPCheckerImpl{
		bypassRanges: make([]string, 0, len(bypassRanges)),
		bypassNets:   make([]*net.IPNet, 0, len(bypassRanges)),
		verboseLog:   verboseLog,
		cache: &lruCache{
			maxSize: defaultCacheSize,
			ttl:     defaultCacheTTL,
			cache:   make(map[string]*list.Element),
			lruList: list.New(),
			enabled: true, // Enable cache by default for performance
		},
	}

	for _, cidr := range bypassRanges {
		if err := checker.AddBypassRange(cidr); err != nil {
			return nil, fmt.Errorf("failed to add bypass IP range '%s': %w. Valid formats: '10.0.0.0/24' (CIDR) or '10.0.0.1' (single IP)", cidr, err)
		}
	}

	return checker, nil
}

// ShouldBypass checks if the given IP address should bypass authentication
func (c *BypassIPCheckerImpl) ShouldBypass(ipStr string) bool {
	if ipStr == "" {
		if c.verboseLog {
			log.Printf("[BYPASS] Empty IP address provided")
		}
		return false
	}

	// Check cache first
	if c.cache != nil && c.cache.enabled {
		if result, found := c.getFromCache(ipStr); found {
			if c.verboseLog {
				log.Printf("[BYPASS] CACHE HIT: IP %s - bypass=%v", ipStr, result)
			}
			return result
		}
		if c.verboseLog {
			log.Printf("[BYPASS] CACHE MISS: IP %s - checking ranges", ipStr)
		}
	}

	clientIP := net.ParseIP(ipStr)
	if clientIP == nil {
		if c.verboseLog {
			log.Printf("[BYPASS] Failed to parse client IP: %s", ipStr)
		}
		// Cache negative result
		if c.cache != nil && c.cache.enabled {
			c.addToCache(ipStr, false)
		}
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check against all bypass networks
	for i, network := range c.bypassNets {
		if network.Contains(clientIP) {
			if c.verboseLog {
				log.Printf("[BYPASS] IP %s matched bypass range %s", ipStr, c.bypassRanges[i])
			}
			// Cache positive result
			if c.cache != nil && c.cache.enabled {
				c.addToCache(ipStr, true)
			}
			return true
		}
	}

	if c.verboseLog {
		log.Printf("[BYPASS] IP %s did not match any bypass range", ipStr)
	}
	// Cache negative result
	if c.cache != nil && c.cache.enabled {
		c.addToCache(ipStr, false)
	}
	return false
}

// AddBypassRange adds a new IP range to the bypass list
func (c *BypassIPCheckerImpl) AddBypassRange(cidr string) error {
	// Parse CIDR notation
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		// Try parsing as single IP
		ip := net.ParseIP(cidr)
		if ip == nil {
			return fmt.Errorf("'%s' is not a valid CIDR notation or IP address. Examples: '10.0.0.0/24' (CIDR), '192.168.1.1' (IPv4), '2001:db8::1' (IPv6)", cidr)
		}
		// Convert single IP to /32 or /128 CIDR
		if ip.To4() != nil {
			cidr = fmt.Sprintf("%s/32", ip.String())
		} else {
			cidr = fmt.Sprintf("%s/128", ip.String())
		}
		_, network, err = net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("internal error converting IP to CIDR for '%s': %w", cidr, err)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	for _, existingCIDR := range c.bypassRanges {
		if existingCIDR == cidr {
			if c.verboseLog {
				log.Printf("[BYPASS] Range %s already exists in bypass list", cidr)
			}
			return nil
		}
	}

	c.bypassRanges = append(c.bypassRanges, cidr)
	c.bypassNets = append(c.bypassNets, network)

	// Clear cache when ranges change
	c.ClearCache()

	if c.verboseLog {
		log.Printf("[BYPASS] Added bypass range: %s", cidr)
	}
	return nil
}

// RemoveBypassRange removes an IP range from the bypass list
func (c *BypassIPCheckerImpl) RemoveBypassRange(cidr string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, existingCIDR := range c.bypassRanges {
		if existingCIDR == cidr {
			// Remove from both slices
			c.bypassRanges = append(c.bypassRanges[:i], c.bypassRanges[i+1:]...)
			c.bypassNets = append(c.bypassNets[:i], c.bypassNets[i+1:]...)

			// Clear cache when ranges change
			c.ClearCache()

			if c.verboseLog {
				log.Printf("[BYPASS] Removed bypass range: %s", cidr)
			}
			return nil
		}
	}

	return fmt.Errorf("bypass range '%s' not found in the current configuration. Use GetBypassRanges() to see active ranges", cidr)
}

// GetBypassRanges returns the current list of bypass IP ranges
func (c *BypassIPCheckerImpl) GetBypassRanges() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]string, len(c.bypassRanges))
	copy(result, c.bypassRanges)
	return result
}

// SetCacheEnabled enables or disables the cache
func (c *BypassIPCheckerImpl) SetCacheEnabled(enabled bool) {
	if c.cache != nil {
		c.cache.mu.Lock()
		c.cache.enabled = enabled
		if !enabled {
			// Clear cache when disabled
			c.cache.cache = make(map[string]*list.Element)
			c.cache.lruList = list.New()
		}
		c.cache.mu.Unlock()
	}
}

// ClearCache clears the IP cache
func (c *BypassIPCheckerImpl) ClearCache() {
	if c.cache != nil {
		c.cache.mu.Lock()
		c.cache.cache = make(map[string]*list.Element)
		c.cache.lruList = list.New()
		c.cache.mu.Unlock()
		if c.verboseLog {
			log.Printf("[BYPASS] INFO: Cache cleared")
		}
	}
}

// getFromCache retrieves a cached result if available and not expired
func (c *BypassIPCheckerImpl) getFromCache(ip string) (bool, bool) {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	if elem, found := c.cache.cache[ip]; found {
		item := elem.Value.(*lruCacheItem)
		// Check if entry is expired
		if time.Since(item.value.timestamp) > c.cache.ttl {
			// Remove expired entry
			c.cache.lruList.Remove(elem)
			delete(c.cache.cache, ip)
			return false, false
		}
		// Move to front (most recently used)
		c.cache.lruList.MoveToFront(elem)
		return item.value.bypass, true
	}
	return false, false
}

// addToCache adds a result to the cache
func (c *BypassIPCheckerImpl) addToCache(ip string, bypass bool) {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	// Check if already in cache
	if elem, found := c.cache.cache[ip]; found {
		// Update existing entry
		item := elem.Value.(*lruCacheItem)
		item.value = cacheEntry{
			bypass:    bypass,
			timestamp: time.Now(),
		}
		c.cache.lruList.MoveToFront(elem)
		return
	}

	// Add new entry
	item := &lruCacheItem{
		key: ip,
		value: cacheEntry{
			bypass:    bypass,
			timestamp: time.Now(),
		},
	}
	elem := c.cache.lruList.PushFront(item)
	c.cache.cache[ip] = elem

	// Evict oldest if cache is full
	if c.cache.lruList.Len() > c.cache.maxSize {
		oldest := c.cache.lruList.Back()
		if oldest != nil {
			oldItem := oldest.Value.(*lruCacheItem)
			delete(c.cache.cache, oldItem.key)
			c.cache.lruList.Remove(oldest)
		}
	}
}
