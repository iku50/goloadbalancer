package goloadbalancer

import (
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// Backend holds the data about a server that
// is required to create proxy
// ReverseProxy is an HTTP Handler that takes an incoming request and
// sends it to another server, proxying the response back to the client.
// eg of ReverseProxy: https://golang.org/pkg/net/http/httputil/#ReverseProxy
type Backend struct {
	// URL of the backend
	URL *url.URL
	// is the backend alive?
	Alive bool
	// mu protects the Alive field
	mu sync.RWMutex
	// the reverse proxy that handles requests
	// to this backend
	Proxy *httputil.ReverseProxy
}

// ServerPool holds information about reachable backends
type ServerPool struct {
	// backends slice
	backends []*Backend
	// current index of the slice
	current uint64
}

// NextIndex atomically increase the counter and return an index
func (s *ServerPool) NextIndex() int {
	// Multiple clients can call this at once
	return int(atomic.AddUint64(&s.current, uint64(1)) % uint64(len(s.backends)))
}

// NextPeer returns next active peer to take a connection
func (s *ServerPool) NextPeer() *Backend {
	next := s.NextIndex()
	l := len(s.backends) + next // start from next and move a full cycle
	for i := next; i < l; i++ {
		index := i % len(s.backends)
		if s.backends[index].IsAlive() {
			// if we have an alive backend, use it and store it's index
			if i != next {
				atomic.StoreUint64(&s.current, uint64(index))
			}
			return s.backends[index]
		}
	}
	return nil
}

// SetAlive for this backend
func (b *Backend) SetAlive(alive bool) {
	b.mu.Lock()
	b.Alive = alive
	b.mu.Unlock()
}

// IsAlive returns true when backend is alive
func (b *Backend) IsAlive() (alive bool) {
	b.mu.RLock()
	alive = b.Alive
	b.mu.RUnlock()
	return
}

// LbHandler is the load balancer handler
// it passes the request to the next available peer
func (s *ServerPool) LbHandler(w http.ResponseWriter, r *http.Request) {
	peer := s.NextPeer()
	if peer != nil {
		peer.Proxy.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Service not available", http.StatusServiceUnavailable)
}

// ActiveHealthCheck finds the selected backend is unresponsive,
// marks it as dead and returns true
// LbHandlerWithHealthCheck is the active health check load balancer handler
// it passes the request to the next available peer
func (s *ServerPool) LbHandlerWithHealthCheck(w http.ResponseWriter, r *http.Request) {
	attempts := GetAttemptsFromContext(r)
	if attempts > 3 {
		http.Error(w, "Service not available", http.StatusServiceUnavailable)
		return
	}
	peer := s.NextPeer()
	if peer != nil {
		peer.Proxy.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Service not available", http.StatusServiceUnavailable)
}

const (
	AttemptsKey int = iota
)

// GetAttemptsFromContext returns the number of attempts
// made to connect to the backend
func GetAttemptsFromContext(r *http.Request) int {
	if attempts, ok := r.Context().Value(AttemptsKey).(int); ok {
		return attempts
	}
	return 0
}

// PassiveHealthCheck checks the health of the backend
// by doing a GET request to /healthz

// isAliveCheck performs a check on a backend and updates its status
func isAliveCheck(u *url.URL) bool {
	timeout := 2 * time.Second
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		log.Println("Site unreachable, error: ", err)
		return false
	}
	_ = conn.Close()
	return true
}

// HealthCheck runs a routine for checking the health of the backends
func (s *ServerPool) HealthCheck() {
	t := time.NewTicker(time.Second * 2)
	for v := range t.C {
		log.Println("Health checkup at ", v)
		for _, b := range s.backends {
			status := isAliveCheck(b.URL)
			b.SetAlive(status)
			if status {
				log.Println(b.URL, "is alive")
			} else {
				log.Println(b.URL, "is dead")
			}
		}
	}
}
