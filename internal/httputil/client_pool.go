package httputil

import (
	"net/http"
	"sync"
)

// ClientPool maintains per-provider HTTP clients so that each upstream
// provider gets its own connection pool. When a provider's connections go
// bad (e.g. Ollama Cloud HTTP/2 stream corruption), only that provider's
// idle connections are evicted — other providers are unaffected.
type ClientPool struct {
	mu      sync.Mutex
	clients map[string]*http.Client
}

// NewClientPool returns an empty ClientPool. Clients are created lazily on
// first use via Get.
func NewClientPool() *ClientPool {
	return &ClientPool{
		clients: make(map[string]*http.Client),
	}
}

// Get returns the HTTP client for the given key. Clients are created lazily
// and cached indefinitely. The key should uniquely identify a provider —
// typically the provider name or backend URL.
func (p *ClientPool) Get(key string) *http.Client {
	p.mu.Lock()
	client, ok := p.clients[key]
	if !ok {
		client = NewHTTPClient()
		p.clients[key] = client
	}
	p.mu.Unlock()

	return client
}

// CloseIdleConnections closes idle connections for the given key only.
// Other providers are unaffected.
func (p *ClientPool) CloseIdleConnections(key string) {
	p.mu.Lock()
	client, ok := p.clients[key]
	p.mu.Unlock()

	if ok {
		client.CloseIdleConnections()
	}
}
