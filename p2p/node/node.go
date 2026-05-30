package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	corep2p "github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"p2p/discovery"
	"p2p/protocol"
	"p2p/transfer"
)

// AppProtocol is the libp2p stream protocol carrying all application messages
// (chat, file transfer, folder sync, peer exchange) using the framing in the
// protocol package.
const AppProtocol = "/p2p-app/1.0.0"

// mdnsServiceTag scopes LAN discovery; only nodes using the same tag find each
// other via mDNS.
const mdnsServiceTag = "p2p-app-mdns"

// Config holds all options for creating a Node.
type Config struct {
	ListenPort    int      // TCP+QUIC listen port (v4 and v6)
	Nick          string   // display name
	DownloadsDir  string   // directory for received files; defaults to "downloads"
	Bootstrap     []string // peer multiaddrs to dial on startup (/ip4/.../p2p/<id>)
	Relays        []string // static relay multiaddrs (optional)
	SharedFolders []string // names of folders to sync with peers
	LAN           bool     // enable mDNS LAN discovery
	Announce      string   // public IP (or ip:port) to advertise as dialable, e.g. "203.0.113.4"
}

// Node is the central P2P actor. It owns the libp2p host, the peer store, and
// all message dispatch logic.
type Node struct {
	cfg   Config
	host  host.Host
	peers *PeerStore
	recv  *transfer.Receiver
	mdns  *discovery.Service

	onChat         []func(nick, text string, ts time.Time)
	onFile         []func(path string)
	onPeer         []func(PeerInfo)
	onPeerLeave    []func(addr string)
	onProgress     []func(name string, sent, total int64, recv bool, peerAddr string)
	onFileAck      []func(peerAddr, id string, ok bool, errMsg string)
	onFolderChange []func(folderName, relPath, absPath string, deleted bool)

	folders map[string]*sharedFolder

	relayMu    sync.Mutex
	relayCands map[peer.ID]peer.AddrInfo // candidate relays surfaced to AutoRelay

	announceMu sync.RWMutex
	announce   string // public IP (or ip:port) advertised as a dialable address

	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	quit      chan struct{}
}

// New creates a Node from cfg. Call Start to begin networking.
func New(cfg Config) (*Node, error) {
	if cfg.DownloadsDir == "" {
		cfg.DownloadsDir = "downloads"
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{
		cfg:        cfg,
		peers:      newPeerStore(),
		recv:       transfer.NewReceiver(cfg.DownloadsDir),
		relayCands: make(map[peer.ID]peer.AddrInfo),
		announce:   strings.TrimSpace(cfg.Announce),
		ctx:        ctx,
		cancel:     cancel,
		quit:       make(chan struct{}),
	}

	// Receiver progress feeds into node-level OnProgress callbacks.
	n.recv.SetProgressFunc(transfer.ProgressFunc(func(name string, sent, total int64, peerAddr string) {
		for _, fn := range n.onProgress {
			fn(name, sent, total, true, peerAddr)
		}
	}))
	// Receiver ACK path: route file_ack messages back to the original sender.
	n.recv.SetAckSender(func(peerAddr string, msg protocol.Message) {
		if p := n.peers.Get(peerAddr); p != nil {
			p.Send(msg)
		}
	})

	h, err := n.buildHost()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}
	n.host = h

	// Seed static relay candidates from config.
	for _, raw := range cfg.Relays {
		if ai := parseAddrInfo(raw); ai != nil {
			n.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.PermanentAddrTTL)
			n.noteRelayCandidate(*ai)
		}
	}

	n.initSharedFolders()
	return n, nil
}

// buildHost constructs the libp2p host with the full automatic NAT-traversal
// stack: all transports over IPv4+IPv6, UPnP/NAT-PMP port mapping, AutoNAT
// reachability detection, DCUtR hole punching, a circuit-relay service (used
// when this node is publicly reachable), and AutoRelay fed by peers we learn
// through gossip (no DHT required).
func (n *Node) buildHost() (host.Host, error) {
	p := n.cfg.ListenPort
	listen := []string{
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", p),
		fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", p),
		fmt.Sprintf("/ip6/::/tcp/%d", p),
		fmt.Sprintf("/ip6/::/udp/%d/quic-v1", p),
	}
	return libp2p.New(
		libp2p.ListenAddrStrings(listen...),
		libp2p.AddrsFactory(n.addrsFactory),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.EnableHolePunching(),
		libp2p.EnableRelay(),
		libp2p.EnableRelayService(),
		libp2p.EnableAutoRelayWithPeerSource(n.relaySource),
	)
}

// addrsFactory augments libp2p's self-reported addresses with the manually
// announced public address (if set). On a full-cone NAT the user knows their
// public IP, so advertising /ip4/<public>/tcp/<port> makes the copy-pasted
// share address directly dialable from across the internet without relying on
// UPnP or AutoNAT having discovered it. The factory is consulted on every
// host.Addrs() call, so SetAnnounce takes effect immediately.
func (n *Node) addrsFactory(addrs []ma.Multiaddr) []ma.Multiaddr {
	extra := n.announceAddrs()
	if len(extra) == 0 {
		return addrs
	}
	out := append([]ma.Multiaddr(nil), addrs...)
	for _, e := range extra {
		dup := false
		for _, a := range out {
			if a.Equal(e) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, e)
		}
	}
	return out
}

// announceAddrs builds the TCP+QUIC multiaddrs for the announced public IP,
// defaulting the port to the listen port when none is given. Returns nil when
// no (valid) announce address is configured.
func (n *Node) announceAddrs() []ma.Multiaddr {
	ann := n.getAnnounce()
	if ann == "" {
		return nil
	}
	host, port := ann, n.cfg.ListenPort
	if h, p, err := net.SplitHostPort(ann); err == nil {
		host = h
		if pp, err := strconv.Atoi(p); err == nil {
			port = pp
		}
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	proto := "ip4"
	if ip.To4() == nil {
		proto = "ip6"
	}
	var out []ma.Multiaddr
	for _, s := range []string{
		fmt.Sprintf("/%s/%s/tcp/%d", proto, host, port),
		fmt.Sprintf("/%s/%s/udp/%d/quic-v1", proto, host, port),
	} {
		if m, err := ma.NewMultiaddr(s); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// getAnnounce returns the currently announced public address (empty if unset).
func (n *Node) getAnnounce() string {
	n.announceMu.RLock()
	defer n.announceMu.RUnlock()
	return n.announce
}

// SetAnnounce updates the announced public address at runtime. Pass "" to clear
// it. The change is reflected immediately in P2pAddrs/ShareAddrs and propagates
// to peers on the next peer-list gossip.
func (n *Node) SetAnnounce(addr string) {
	n.announceMu.Lock()
	n.announce = strings.TrimSpace(addr)
	n.announceMu.Unlock()
}

// AnnounceAddr returns the configured public address override (empty if unset).
func (n *Node) AnnounceAddr() string { return n.getAnnounce() }

// relaySource implements autorelay.PeerSource: it yields known relay candidates
// (static relays plus connected peers with public addresses) and then closes
// the channel, as AutoRelay expects.
func (n *Node) relaySource(ctx context.Context, num int) <-chan peer.AddrInfo {
	out := make(chan peer.AddrInfo)
	go func() {
		defer close(out)
		n.relayMu.Lock()
		cands := make([]peer.AddrInfo, 0, len(n.relayCands))
		for _, ai := range n.relayCands {
			cands = append(cands, ai)
		}
		n.relayMu.Unlock()
		for i, ai := range cands {
			if i >= num {
				return
			}
			select {
			case out <- ai:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// noteRelayCandidate records a peer as a potential relay if it advertises any
// public address.
func (n *Node) noteRelayCandidate(ai peer.AddrInfo) {
	if ai.ID == "" {
		return
	}
	public := make([]ma.Multiaddr, 0, len(ai.Addrs))
	for _, a := range ai.Addrs {
		if manet.IsPublicAddr(a) {
			public = append(public, a)
		}
	}
	if len(public) == 0 {
		return
	}
	n.relayMu.Lock()
	n.relayCands[ai.ID] = peer.AddrInfo{ID: ai.ID, Addrs: public}
	n.relayMu.Unlock()
}

// --- callback registration (unchanged API) ---

func (n *Node) OnChat(fn func(nick, text string, ts time.Time)) { n.onChat = append(n.onChat, fn) }
func (n *Node) OnFile(fn func(path string))                     { n.onFile = append(n.onFile, fn) }
func (n *Node) OnPeer(fn func(PeerInfo))                        { n.onPeer = append(n.onPeer, fn) }
func (n *Node) OnPeerLeave(fn func(addr string))               { n.onPeerLeave = append(n.onPeerLeave, fn) }

func (n *Node) OnFolderChange(fn func(folderName, relPath, absPath string, deleted bool)) {
	n.onFolderChange = append(n.onFolderChange, fn)
}

func (n *Node) OnProgress(fn func(name string, sent, total int64, recv bool, peerAddr string)) {
	n.onProgress = append(n.onProgress, fn)
	n.recv.SetProgressFunc(transfer.ProgressFunc(func(name string, sent, total int64, peerAddr string) {
		for _, fn := range n.onProgress {
			fn(name, sent, total, true, peerAddr)
		}
	}))
}

func (n *Node) OnFileAck(fn func(peerAddr, id string, ok bool, errMsg string)) {
	n.onFileAck = append(n.onFileAck, fn)
}

// SharedFolderInfo summarises a shared folder exposed by this node.
type SharedFolderInfo struct {
	Name string
	Dir  string
}

// SharedFolderInfos returns the list of folders this node is sharing.
func (n *Node) SharedFolderInfos() []SharedFolderInfo {
	result := make([]SharedFolderInfo, 0, len(n.folders))
	for _, sf := range n.folders {
		result = append(result, SharedFolderInfo{Name: sf.name, Dir: sf.dir})
	}
	return result
}

// Nick returns this node's display name.
func (n *Node) Nick() string { return n.cfg.Nick }

// ID returns this node's libp2p peer ID string.
func (n *Node) ID() string { return n.host.ID().String() }

// ListenAddr returns a human-readable summary of the addresses this node is
// reachable on.
func (n *Node) ListenAddr() string { return strings.Join(n.P2pAddrs(), ", ") }

// CryptoEnabled reports whether connections are encrypted. With libp2p every
// connection is encrypted and peer-authenticated, so this is always true.
func (n *Node) CryptoEnabled() bool { return true }

// DownloadsDir returns the directory where received files are written.
func (n *Node) DownloadsDir() string { return n.cfg.DownloadsDir }

// ExternalAddr returns this node's public (internet-facing) multiaddrs, if any
// have been discovered, joined by commas. Empty if only private/relay addrs.
func (n *Node) ExternalAddr() string {
	var pub []string
	for _, a := range n.host.Addrs() {
		if manet.IsPublicAddr(a) {
			pub = append(pub, a.String())
		}
	}
	return strings.Join(pub, ", ")
}

// P2pAddrs returns this node's fully-qualified dialable addresses, each ending
// in /p2p/<id>. Any of these can be handed to a peer as a --bootstrap value.
func (n *Node) P2pAddrs() []string {
	info := peer.AddrInfo{ID: n.host.ID(), Addrs: n.host.Addrs()}
	p2pAddrs, err := peer.AddrInfoToP2pAddrs(&info)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(p2pAddrs))
	for _, a := range p2pAddrs {
		out = append(out, a.String())
	}
	return out
}

// ShareAddrs returns this node's dialable /p2p addresses ordered for sharing:
// the manually announced address first (the user explicitly set it as their
// reachable public IP), then any auto-discovered public addresses, then private
// LAN, then link-local, then loopback. The first entry is the best one to hand
// to a peer across the internet.
func (n *Node) ShareAddrs() []string {
	addrs := n.P2pAddrs()
	// Set of announced IPs (without /p2p) so we can rank them top regardless of
	// whether the IP falls in a range manet considers "public".
	announced := make(map[string]bool)
	for _, a := range n.announceAddrs() {
		if ip, err := manet.ToIP(a); err == nil {
			announced[ip.String()] = true
		}
	}
	rank := func(s string) int {
		m, err := ma.NewMultiaddr(s)
		if err != nil {
			return 5
		}
		if ip, err := manet.ToIP(m); err == nil && announced[ip.String()] {
			return 0
		}
		switch {
		case manet.IsPublicAddr(m):
			return 1
		case manet.IsIPLoopback(m):
			return 4
		case manet.IsIP6LinkLocal(m) || isIP4LinkLocal(m):
			return 3
		default:
			return 2 // private LAN (10/8, 192.168/16, 172.16/12, etc.)
		}
	}
	sort.SliceStable(addrs, func(i, j int) bool { return rank(addrs[i]) < rank(addrs[j]) })
	return addrs
}

// isIP4LinkLocal reports whether m carries an IPv4 link-local (169.254/16)
// address — the APIPA range that is never useful to share.
func isIP4LinkLocal(m ma.Multiaddr) bool {
	ip, err := manet.ToIP(m)
	if err != nil {
		return false
	}
	v4 := ip.To4()
	return v4 != nil && v4[0] == 169 && v4[1] == 254
}

// BestShareAddr returns the single most-dialable address (the first ShareAddrs
// entry), or "" if the node has no addresses yet.
func (n *Node) BestShareAddr() string {
	if a := n.ShareAddrs(); len(a) > 0 {
		return a[0]
	}
	return ""
}

// Start registers the stream handler, wires connection notifications, starts
// LAN discovery, dials bootstrap peers, and launches the gossip loop.
func (n *Node) Start() error {
	n.host.SetStreamHandler(corep2p.ID(AppProtocol), n.handleStream)

	// When a connection comes up, the peer with the lexicographically-smaller
	// ID opens the application stream; the other side accepts it via the
	// handler. This deterministic tie-break avoids duplicate streams.
	n.host.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(_ network.Network, c network.Conn) {
			remote := c.RemotePeer()
			if n.host.ID().String() < remote.String() {
				go n.openStream(remote)
			}
		},
	})

	if n.cfg.LAN {
		svc, err := discovery.New(n.host, mdnsServiceTag, func(pi peer.AddrInfo) {
			if pi.ID == n.host.ID() {
				return
			}
			n.host.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstore.AddressTTL)
			_ = n.host.Connect(n.ctx, pi)
		})
		if err != nil {
			fmt.Printf("[warn] LAN discovery unavailable: %v\n", err)
		} else {
			n.mdns = svc
		}
	}

	for _, addr := range n.cfg.Bootstrap {
		if err := n.Bootstrap(addr); err != nil {
			fmt.Printf("[warn] bootstrap %s: %v\n", addr, err)
		}
	}

	go n.gossipLoop()
	return nil
}

// Bootstrap dials a peer given as a multiaddr (/ip4/.../p2p/<id>) and joins the
// swarm; subsequent peer-exchange gossip grows the mesh.
func (n *Node) Bootstrap(addr string) error { return n.Connect(addr) }

// Connect dials a peer given as a multiaddr.
func (n *Node) Connect(addr string) error {
	ai := parseAddrInfo(addr)
	if ai == nil {
		return fmt.Errorf("invalid peer multiaddr %q (expected /ip4/.../p2p/<id>)", addr)
	}
	n.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.PermanentAddrTTL)
	if err := n.host.Connect(n.ctx, *ai); err != nil {
		return fmt.Errorf("connect %s: %w", ai.ID, err)
	}
	return nil
}

// SendChat broadcasts a text message to all connected peers.
func (n *Node) SendChat(text string) {
	msg, err := protocol.NewChatMessage(n.cfg.Nick, text)
	if err != nil {
		return
	}
	n.broadcast(msg)
}

// SendFile sends a file to a specific peer identified by its peer ID string.
func (n *Node) SendFile(addr, path string) error {
	p := n.peers.Get(addr)
	if p == nil {
		return fmt.Errorf("peer %s not connected", addr)
	}
	name := filepath.Base(path)
	progressFn := transfer.ProgressFunc(func(_ string, sent, total int64, peerAddr string) {
		for _, fn := range n.onProgress {
			fn(name, sent, total, false, peerAddr)
		}
	})
	return transfer.Send(path, p.Addr, p.Send, progressFn)
}

// FolderFileEntry describes one file in an ad-hoc folder send.
type FolderFileEntry struct {
	AbsPath string
	RelPath string // forward-slash relative path inside the folder
	ModTime int64  // unix seconds; 0 to use the file's mtime
}

// SendFolder sends each entry to a peer as a folder transfer, preserving
// relative paths under folderName.
func (n *Node) SendFolder(addr, folderName string, entries []FolderFileEntry) error {
	p := n.peers.Get(addr)
	if p == nil {
		return fmt.Errorf("peer %s not connected", addr)
	}
	progressFn := transfer.ProgressFunc(func(relPath string, sent, total int64, peerAddr string) {
		name := folderName + "/" + relPath
		for _, fn := range n.onProgress {
			fn(name, sent, total, false, peerAddr)
		}
	})
	for _, e := range entries {
		mt := e.ModTime
		if mt == 0 {
			if info, err := os.Stat(e.AbsPath); err == nil {
				mt = info.ModTime().Unix()
			}
		}
		if err := transfer.SendFolderFile(folderName, e.RelPath, e.AbsPath, mt, p.Addr, p.Send, progressFn); err != nil {
			return fmt.Errorf("send %s: %w", e.RelPath, err)
		}
	}
	return nil
}

// Peers returns a snapshot of connected peer addresses and nicknames.
func (n *Node) Peers() []PeerInfo {
	list := n.peers.List()
	out := make([]PeerInfo, 0, len(list))
	for _, p := range list {
		out = append(out, PeerInfo{Addr: p.Addr, Nick: p.Nick, Crypto: true, ExtAddr: p.ExtAddr})
	}
	return out
}

// Close gracefully shuts down the node, all peer connections, and the host.
func (n *Node) Close() {
	n.closeOnce.Do(func() {
		close(n.quit)
		n.cancel()
		if n.mdns != nil {
			n.mdns.Stop()
		}
		for _, p := range n.peers.List() {
			p.Close()
		}
		for _, sf := range n.folders {
			sf.watcher.Close()
		}
		if n.host != nil {
			n.host.Close()
		}
	})
}

// PeerInfo is a summary of a connected peer (safe to expose outside the package).
type PeerInfo struct {
	Addr    string `json:"addr"` // remote peer.ID string
	Nick    string `json:"nick"`
	Crypto  bool   `json:"crypto"`
	ExtAddr string `json:"ext_addr,omitempty"`
}

// --- internal: stream / peer lifecycle ---

// handleStream is invoked for inbound app streams (opened by the peer with the
// smaller ID).
func (n *Node) handleStream(s network.Stream) {
	n.setupPeer(s)
}

// openStream dials the app protocol to a peer we are connected to. Called only
// by the smaller-ID side (see Start's notifiee).
func (n *Node) openStream(pid peer.ID) {
	if n.peers.Has(pid.String()) {
		return
	}
	ctx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
	defer cancel()
	s, err := n.host.NewStream(ctx, pid, corep2p.ID(AppProtocol))
	if err != nil {
		return // peer may not run our protocol, or connection dropped
	}
	n.setupPeer(s)
}

// setupPeer performs the app-level hello (nick exchange) on a freshly opened
// stream and, if this is the first stream for the peer, registers it and starts
// its read/write loops.
func (n *Node) setupPeer(s network.Stream) {
	pid := s.Conn().RemotePeer()
	p := newPeer(s, pid)

	// --- app hello: exchange nicks. libp2p already handled auth/encryption. ---
	_ = s.SetDeadline(time.Now().Add(15 * time.Second))
	hello, err := protocol.NewMessage(protocol.MsgHandshake, protocol.HandshakePayload{Nick: n.cfg.Nick})
	if err != nil || protocol.WriteMessage(s, hello) != nil {
		_ = s.Reset()
		return
	}
	resp, err := protocol.ReadMessage(s)
	if err != nil || resp.Type != protocol.MsgHandshake {
		_ = s.Reset()
		return
	}
	var their protocol.HandshakePayload
	if err := json.Unmarshal(resp.Payload, &their); err != nil {
		_ = s.Reset()
		return
	}
	_ = s.SetDeadline(time.Time{})
	p.Nick = their.Nick
	if pub := firstPublicAddr(n.host.Peerstore().Addrs(pid)); pub != "" {
		p.ExtAddr = pub
	}

	// Avoid duplicate peers (e.g. a race where both sides opened a stream).
	if n.peers.Has(p.Addr) {
		_ = s.Reset()
		return
	}
	n.peers.Add(p)
	go p.writeLoop()
	go p.readLoop(n.handleMessage)

	for _, fn := range n.onPeer {
		fn(PeerInfo{Addr: p.Addr, Nick: p.Nick, Crypto: true, ExtAddr: p.ExtAddr})
	}

	// Announce shared folders and ask the new peer for its peer list so the
	// mesh grows quickly.
	go n.sendFolderAnnounce(p)
	if req, err := protocol.NewMessage(protocol.MsgPeerListReq, struct{}{}); err == nil {
		p.Send(req)
	}

	// Clean up when the peer disconnects.
	go func() {
		<-p.Done()
		n.peers.Remove(p.Addr)
		n.recv.AbortPeer(p.Addr)
		fmt.Printf("[info] peer %s (%s) disconnected\n", p.Nick, p.Addr)
		for _, fn := range n.onPeerLeave {
			fn(p.Addr)
		}
		for _, sf := range n.folders {
			sf.removePeer(p.Addr)
		}
	}()
}

// --- internal: gossip / message dispatch ---

// gossipLoop periodically asks peers for their peer lists, helping late joiners
// discover the rest of the swarm and refreshing relay candidates.
func (n *Node) gossipLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.quit:
			return
		case <-ticker.C:
			msg, _ := protocol.NewMessage(protocol.MsgPeerListReq, struct{}{})
			n.broadcast(msg)
		}
	}
}

func (n *Node) handleMessage(p *Peer, msg protocol.Message) {
	switch msg.Type {
	case protocol.MsgChat:
		n.handleChat(msg)
	case protocol.MsgFileMeta:
		n.recv.HandleMeta(msg.Payload, p.Addr, func(path string) {
			for _, fn := range n.onFile {
				fn(path)
			}
		})
	case protocol.MsgFileChunk:
		n.recv.HandleChunk(msg.Payload, msg.Bin)
	case protocol.MsgFileChecksum:
		n.recv.HandleChecksum(msg.Payload)
	case protocol.MsgFileAck:
		n.handleFileAck(p, msg)
	case protocol.MsgPeerListReq:
		n.handlePeerListReq(p)
	case protocol.MsgPeerListRes:
		n.handlePeerListRes(p, msg)
	case protocol.MsgFolderAnnounce:
		n.handleFolderAnnounce(p, msg)
	case protocol.MsgFolderFileMeta:
		n.handleFolderFileMeta(p, msg)
	case protocol.MsgFolderDelete:
		n.handleFolderDelete(msg)
	}
}

func (n *Node) handleChat(msg protocol.Message) {
	var chat protocol.ChatPayload
	if err := json.Unmarshal(msg.Payload, &chat); err != nil {
		return
	}
	for _, fn := range n.onChat {
		fn(chat.Nick, chat.Text, time.Unix(chat.Time, 0))
	}
}

func (n *Node) handleFileAck(p *Peer, msg protocol.Message) {
	var ack protocol.FileAckPayload
	if err := json.Unmarshal(msg.Payload, &ack); err != nil {
		return
	}
	if ack.OK {
		fmt.Printf("[ack] peer %s confirmed file %s\n", p.Addr, ack.ID)
	} else {
		fmt.Printf("[ack] peer %s reported FAILED file %s: %s\n", p.Addr, ack.ID, ack.Err)
	}
	for _, fn := range n.onFileAck {
		fn(p.Addr, ack.ID, ack.OK, ack.Err)
	}
}

// handlePeerListReq replies with the peers we know (each with their multiaddrs)
// plus our own addresses, so the requester can dial them directly or via relay.
func (n *Node) handlePeerListReq(requester *Peer) {
	var peers []protocol.PeerAddrInfo
	for _, p := range n.peers.List() {
		if p.Addr == requester.Addr {
			continue
		}
		peers = append(peers, addrInfoToWire(p.pid, n.host.Peerstore().Addrs(p.pid)))
	}
	// Include ourselves so the requester learns all our addrs (incl. relay).
	peers = append(peers, addrInfoToWire(n.host.ID(), n.host.Addrs()))

	resp, err := protocol.NewMessage(protocol.MsgPeerListRes, protocol.PeerListPayload{Peers: peers})
	if err != nil {
		return
	}
	requester.Send(resp)
}

// handlePeerListRes ingests a peer list: records addresses, notes relay
// candidates, and dials peers we are not yet connected to. libp2p races all
// known transports/addresses (and relay + hole punch) automatically.
func (n *Node) handlePeerListRes(_ *Peer, msg protocol.Message) {
	var pl protocol.PeerListPayload
	if err := json.Unmarshal(msg.Payload, &pl); err != nil {
		return
	}
	for _, wire := range pl.Peers {
		ai := wireToAddrInfo(wire)
		if ai == nil || ai.ID == n.host.ID() {
			continue
		}
		n.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.AddressTTL)
		n.noteRelayCandidate(*ai)
		if !n.peers.Has(ai.ID.String()) {
			go func(target peer.AddrInfo) {
				ctx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
				defer cancel()
				if err := n.host.Connect(ctx, target); err != nil {
					fmt.Printf("[info] could not connect to discovered peer %s: %v\n", target.ID, err)
				}
			}(*ai)
		}
	}
}

func (n *Node) broadcast(msg protocol.Message) {
	for _, p := range n.peers.List() {
		p.Send(msg)
	}
}

// --- helpers ---

// parseAddrInfo parses a /p2p multiaddr (e.g. /ip4/1.2.3.4/tcp/9000/p2p/<id>)
// into a peer.AddrInfo, or returns nil if it is not a valid peer address.
func parseAddrInfo(s string) *peer.AddrInfo {
	m, err := ma.NewMultiaddr(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	ai, err := peer.AddrInfoFromP2pAddr(m)
	if err != nil {
		return nil
	}
	return ai
}

// addrInfoToWire converts a peer ID and its multiaddrs into the wire form.
func addrInfoToWire(pid peer.ID, addrs []ma.Multiaddr) protocol.PeerAddrInfo {
	strs := make([]string, 0, len(addrs))
	for _, a := range addrs {
		strs = append(strs, a.String())
	}
	return protocol.PeerAddrInfo{ID: pid.String(), Addrs: strs}
}

// wireToAddrInfo converts the wire form back into a peer.AddrInfo, or nil on
// malformed input.
func wireToAddrInfo(w protocol.PeerAddrInfo) *peer.AddrInfo {
	pid, err := peer.Decode(w.ID)
	if err != nil {
		return nil
	}
	addrs := make([]ma.Multiaddr, 0, len(w.Addrs))
	for _, s := range w.Addrs {
		if a, err := ma.NewMultiaddr(s); err == nil {
			addrs = append(addrs, a)
		}
	}
	return &peer.AddrInfo{ID: pid, Addrs: addrs}
}

// firstPublicAddr returns the first public multiaddr as a string, or "".
func firstPublicAddr(addrs []ma.Multiaddr) string {
	for _, a := range addrs {
		if manet.IsPublicAddr(a) {
			return a.String()
		}
	}
	return ""
}
