package credentials

import (
	"sync"
)

func newCache() *cache {
	return &cache{
		cache: make(map[string]map[string]string),
	}
}

type cache struct {
	cacheMu sync.RWMutex
	cache   map[string]map[string]string
}

func (g *cache) addToCache(id string, credentials map[string]string) {
	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()
	g.cache[id] = credentials
}

func (g *cache) getFromCache(id string) (credentials map[string]string, ok bool) {
	g.cacheMu.RLock()
	defer g.cacheMu.RUnlock()
	credentials, ok = g.cache[id]
	return
}
