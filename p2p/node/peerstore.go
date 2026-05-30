package node

import "sync"

// PeerStore is a thread-safe registry of connected peers, keyed by their
// canonical listener address ("host:port").
type PeerStore struct {
	mu    sync.RWMutex
	peers map[string]*Peer
}

func newPeerStore() *PeerStore {
	return &PeerStore{peers: make(map[string]*Peer)}
}

// Add inserts a peer. No-op if the address is already present.
func (s *PeerStore) Add(p *Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.peers[p.Addr]; !exists {
		s.peers[p.Addr] = p
	}
}

// Remove deletes a peer by address.
func (s *PeerStore) Remove(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.peers, addr)
}

// Get returns the peer for addr, or nil.
func (s *PeerStore) Get(addr string) *Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.peers[addr]
}

// Has reports whether addr is in the store.
func (s *PeerStore) Has(addr string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.peers[addr]
	return ok
}

// List returns a snapshot of all current peers.
func (s *PeerStore) List() []*Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}
	return out
}
