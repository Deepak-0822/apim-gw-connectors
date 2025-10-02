package discovery

import (
	"strconv"
	"sync"
	"time"

	"github.com/wso2-extensions/apim-gw-connectors/eg/gateway-connector/internal/constants"
)

type InProgressCache struct {
	mu    sync.Mutex
	store map[string]time.Time
	ttl   time.Duration
}

var cache *InProgressCache

func init() {
	expiryTimeInSecondsStr := getEnvOrDefault(constants.EnvInProgressExpiryKey, constants.DefaultInProgressExpiry)
	expiryTimeInSeconds, err := strconv.Atoi(expiryTimeInSecondsStr)
	if err != nil {
		expiryTimeInSeconds = 30 
	}
	cache = NewInProgress(time.Duration(expiryTimeInSeconds) * time.Second)
}


// NewInProgress creates a new in-progress tracker with TTL
func NewInProgress(ttl time.Duration) *InProgressCache {
	ip := &InProgressCache{
		store: make(map[string]time.Time),
		ttl:   ttl,
	}

	// Start a background cleaner
	go ip.cleanupLoop()

	return ip
}

// Add marks a hash as in-progress
func (ip *InProgressCache) Add(hash string) {
	ip.mu.Lock()
	defer ip.mu.Unlock()
	ip.store[hash] = time.Now()
}

// Exists checks if a hash is still in progress (and not expired)
func (ip *InProgressCache) Exists(hash string) bool {
	ip.mu.Lock()
	defer ip.mu.Unlock()

	start, ok := ip.store[hash]
	if !ok {
		return false
	}
	if time.Since(start) > ip.ttl {
		// expired → remove it
		delete(ip.store, hash)
		return false
	}
	return true
}

// Delete removes a hash explicitly
func (ip *InProgressCache) Delete(hash string) {
	ip.mu.Lock()
	defer ip.mu.Unlock()
	delete(ip.store, hash)
}

// cleanupLoop periodically removes expired entries
func (ip *InProgressCache) cleanupLoop() {
	ticker := time.NewTicker(ip.ttl / 2)
	defer ticker.Stop()

	for range ticker.C {
		ip.mu.Lock()
		now := time.Now()
		for hash, start := range ip.store {
			if now.Sub(start) > ip.ttl {
				delete(ip.store, hash)
			}
		}
		ip.mu.Unlock()
	}
}