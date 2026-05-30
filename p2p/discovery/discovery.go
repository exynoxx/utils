// Package discovery provides zero-configuration LAN peer discovery via libp2p
// mDNS. Nodes using the same service tag find each other on the local network
// and the caller is notified of each discovered peer.
package discovery

import (
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

// Service wraps a running mDNS discovery service.
type Service struct {
	mdns mdns.Service
}

// notifee adapts an onPeer callback to the mdns.Notifee interface.
type notifee struct {
	onPeer func(peer.AddrInfo)
}

func (n notifee) HandlePeerFound(pi peer.AddrInfo) { n.onPeer(pi) }

// New starts mDNS discovery for h under serviceTag. onPeer is invoked (in its
// own goroutine, per libp2p) for every peer found on the LAN.
func New(h host.Host, serviceTag string, onPeer func(peer.AddrInfo)) (*Service, error) {
	svc := mdns.NewMdnsService(h, serviceTag, notifee{onPeer: onPeer})
	if err := svc.Start(); err != nil {
		return nil, err
	}
	return &Service{mdns: svc}, nil
}

// Stop shuts down the discovery service.
func (s *Service) Stop() {
	if s.mdns != nil {
		_ = s.mdns.Close()
	}
}
