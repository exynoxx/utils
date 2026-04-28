// Package discovery implements UDP-broadcast LAN peer discovery.
// Each node periodically announces itself on a shared UDP port; any node that
// hears an announcement it has not seen before dials the sender.
//
// All nodes on the same LAN must use the same disco port (default 9009).
// Self-announcements are silently dropped by comparing the sender's IP
// against all local interface addresses.
package discovery

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const announceInterval = 5 * time.Second

// announcement is the UDP broadcast payload.
type announcement struct {
	Nick       string `json:"nick"`
	ListenPort int    `json:"listen_port"`
}

// Service broadcasts this node's presence on the LAN and notifies the caller
// of newly discovered peers.
type Service struct {
	nick       string
	listenPort int
	discoPort  int
	onPeer     func(addr string)
	quit       chan struct{}
}

// New returns a Service ready to Start.
// nick and listenPort describe this node; discoPort is the shared UDP port all
// peers on the LAN listen and send on; onPeer is called with "host:tcpPort"
// for each newly-seen peer (called in its own goroutine).
func New(nick string, listenPort, discoPort int, onPeer func(addr string)) *Service {
	return &Service{
		nick:       nick,
		listenPort: listenPort,
		discoPort:  discoPort,
		onPeer:     onPeer,
		quit:       make(chan struct{}),
	}
}

// Start opens the shared UDP socket and launches the send and receive loops.
// Returns an error if the socket cannot be bound (e.g. port already in use).
func (s *Service) Start() error {
	conn, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", s.discoPort))
	if err != nil {
		return fmt.Errorf("discovery listen :%d: %w", s.discoPort, err)
	}
	go s.sendLoop(conn)
	go s.recvLoop(conn)
	return nil
}

// Stop shuts down the discovery service.
func (s *Service) Stop() {
	close(s.quit)
}

func (s *Service) sendLoop(conn net.PacketConn) {
	defer conn.Close()
	bcast := &net.UDPAddr{IP: net.IPv4bcast, Port: s.discoPort}
	payload, _ := json.Marshal(announcement{Nick: s.nick, ListenPort: s.listenPort})

	tick := time.NewTicker(announceInterval)
	defer tick.Stop()

	// Announce immediately so peers don't have to wait for the first tick.
	_, _ = conn.WriteTo(payload, bcast)

	for {
		select {
		case <-s.quit:
			return
		case <-tick.C:
			_, _ = conn.WriteTo(payload, bcast)
		}
	}
}

func (s *Service) recvLoop(conn net.PacketConn) {
	localAddrs := localIPSet()
	buf := make([]byte, 512)

	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.quit:
				// sendLoop already closed conn; clean exit.
			default:
				fmt.Printf("[warn] discovery recv: %v\n", err)
			}
			return
		}

		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok {
			continue
		}

		var ann announcement
		if err := json.Unmarshal(buf[:n], &ann); err != nil {
			continue
		}

		// Ignore our own broadcasts.
		if localAddrs[udpAddr.IP.String()] && ann.ListenPort == s.listenPort {
			continue
		}

		// Call onPeer for every announcement; the node's peer store deduplicates
		// already-connected peers, and this lets us reconnect after a disconnect.
		peerAddr := net.JoinHostPort(udpAddr.IP.String(), fmt.Sprintf("%d", ann.ListenPort))
		go s.onPeer(peerAddr)
	}
}

// localIPSet returns a set of all IPv4/IPv6 addresses assigned to local interfaces.
func localIPSet() map[string]bool {
	ips := make(map[string]bool)
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil {
				ips[ip.String()] = true
			}
		}
	}
	return ips
}
