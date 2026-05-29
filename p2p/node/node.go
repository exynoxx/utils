package node

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"p2p/holepunch"
	"p2p/protocol"
	"p2p/transfer"
)

// Config holds all options for creating a Node.
type Config struct {
	ListenAddr    string // e.g. ":9000"
	Nick          string
	Crypto        bool
	DownloadsDir  string   // directory for received files; defaults to "downloads"
	ExternalAddr  string   // internet-facing addr; overridden by STUN when STUNServer is set
	STUNServer    string   // e.g. "stun.l.google.com:19302"; empty = no STUN / no hole-punch
	SharedFolders []string // names of folders to sync with peers
}

// Node is the central P2P actor. It owns the TCP listener, the peer store,
// and all message dispatch logic.
type Node struct {
	cfg       Config
	keyPair   *protocol.KeyPair // non-nil when cfg.Crypto == true
	peers     *PeerStore
	recv      *transfer.Receiver
	hpSession *holepunch.Session // non-nil when STUN/hole-punch is enabled
	pendingPunches sync.Map       // token(uint64) → chan protocol.HolePunchAckPayload
	onChat      []func(nick, text string, ts time.Time)
	onFile      []func(path string)
	onPeer      []func(PeerInfo) // notified when a new peer connects
	onPeerLeave []func(addr string) // notified when a peer disconnects
	onProgress  []func(name string, sent, total int64, recv bool, peerAddr string) // transfer progress
	onFolderChange []func(folderName, relPath, absPath string, deleted bool)
	folders   map[string]*sharedFolder
	closeOnce sync.Once
	quit      chan struct{}
}

// New creates a Node from cfg. Call Start to begin listening.
func New(cfg Config) (*Node, error) {
	if cfg.DownloadsDir == "" {
		cfg.DownloadsDir = "downloads"
	}
	n := &Node{
		cfg:   cfg,
		peers: newPeerStore(),
		recv:  transfer.NewReceiver(cfg.DownloadsDir),
		quit:  make(chan struct{}),
	}
	// Receiver progress feeds into node-level OnProgress callbacks.
	n.recv.SetProgressFunc(transfer.ProgressFunc(func(name string, sent, total int64, peerAddr string) {
		for _, fn := range n.onProgress {
			fn(name, sent, total, true, peerAddr)
		}
	}))
	if cfg.Crypto {
		kp, err := protocol.GenerateKeyPair()
		if err != nil {
			return nil, fmt.Errorf("generate key pair: %w", err)
		}
		n.keyPair = kp
	}
	n.initSharedFolders()
	return n, nil
}

// OnChat registers a callback invoked for each incoming chat message.
func (n *Node) OnChat(fn func(nick, text string, ts time.Time)) { n.onChat = append(n.onChat, fn) }

// OnFile registers a callback invoked when a file transfer completes.
func (n *Node) OnFile(fn func(path string)) { n.onFile = append(n.onFile, fn) }

// OnPeer registers a callback invoked when a new peer connects.
func (n *Node) OnPeer(fn func(PeerInfo)) { n.onPeer = append(n.onPeer, fn) }

// OnPeerLeave registers a callback invoked when a peer disconnects.
func (n *Node) OnPeerLeave(fn func(addr string)) { n.onPeerLeave = append(n.onPeerLeave, fn) }

// OnFolderChange registers a callback invoked when a shared folder file changes.
// deleted=true means the file was removed; absPath will be empty in that case.
func (n *Node) OnFolderChange(fn func(folderName, relPath, absPath string, deleted bool)) {
	n.onFolderChange = append(n.onFolderChange, fn)
}

// OnProgress registers a callback invoked during file transfers with the
// current byte count, total size, and the peer involved. recv=true means we
// are receiving, false=sending.
func (n *Node) OnProgress(fn func(name string, sent, total int64, recv bool, peerAddr string)) {
	n.onProgress = append(n.onProgress, fn)
	// Wire new callback into receiver immediately so in-progress receives pick it up.
	n.recv.SetProgressFunc(transfer.ProgressFunc(func(name string, sent, total int64, peerAddr string) {
		for _, fn := range n.onProgress {
			fn(name, sent, total, true, peerAddr)
		}
	}))
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

// ListenAddr returns the address this node is listening on.
func (n *Node) ListenAddr() string { return n.cfg.ListenAddr }

// CryptoEnabled reports whether NaCl encryption is configured for this node.
func (n *Node) CryptoEnabled() bool { return n.cfg.Crypto }

// DownloadsDir returns the directory where received files are written.
func (n *Node) DownloadsDir() string { return n.cfg.DownloadsDir }

// ExternalAddr returns the STUN-discovered internet-facing address, or empty.
func (n *Node) ExternalAddr() string { return n.cfg.ExternalAddr }

// Start begins listening for inbound connections on cfg.ListenAddr.
// If cfg.STUNServer is set, Start also binds a UDP socket to the same port,
// performs STUN discovery, and updates cfg.ExternalAddr.
func (n *Node) Start() error {
	ln, err := net.Listen("tcp", n.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", n.cfg.ListenAddr, err)
	}

	// Bind a UDP socket to the same port for hole-punch / STUN.
	// Failure here is non-fatal; hole-punch will be unavailable.
	if n.cfg.STUNServer != "" {
		sess, err := holepunch.NewSession(n.cfg.ListenAddr, n.cfg.STUNServer)
		if err != nil {
			fmt.Printf("[warn] hole-punch session on %s: %v\n", n.cfg.ListenAddr, err)
		} else {
			n.hpSession = sess
			if ext := sess.ExternalAddr(); ext != "" {
				n.cfg.ExternalAddr = ext
				fmt.Printf("[info] external UDP addr (STUN): %s\n", ext)
			}
		}
	}

	go n.acceptLoop(ln)
	go n.gossipLoop()
	return nil
}

// Bootstrap dials addr, performs the handshake, and requests the peer list
// so the node can join the existing swarm by knowing only one address.
func (n *Node) Bootstrap(addr string) error {
	return n.dial(addr, true)
}

// Connect dials addr and performs the handshake.
func (n *Node) Connect(addr string) error {
	return n.dial(addr, false)
}

// SendChat broadcasts a text message to all connected peers.
func (n *Node) SendChat(text string) {
	msg, err := protocol.NewChatMessage(n.cfg.Nick, text)
	if err != nil {
		return
	}
	n.broadcast(msg)
}

// SendFile sends a file to a specific peer identified by addr.
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

// SendFolder sends each entry to peer addr as a folder transfer, preserving
// relative paths under folderName. The receiver places files into its
// downloads/<folderName>/<relPath> (see node.handleFolderFileMeta).
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
		out = append(out, PeerInfo{Addr: p.Addr, Nick: p.Nick, Crypto: p.HasCrypto(), ExtAddr: p.ExtAddr})
	}
	return out
}

// Close gracefully shuts down the node and all peer connections.
func (n *Node) Close() {
	n.closeOnce.Do(func() {
		close(n.quit)
		for _, p := range n.peers.List() {
			p.Close()
		}
		for _, sf := range n.folders {
			sf.watcher.Close()
		}
		if n.hpSession != nil {
			n.hpSession.Close()
		}
	})
}

// PeerInfo is a summary of a connected peer (safe to expose outside the package).
type PeerInfo struct {
	Addr     string `json:"addr"`
	Nick     string `json:"nick"`
	Crypto   bool   `json:"crypto"`
	ExtAddr  string `json:"ext_addr,omitempty"`
}

// --- internal ---

func (n *Node) acceptLoop(ln net.Listener) {
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-n.quit:
				return
			default:
				fmt.Printf("[warn] accept: %v\n", err)
				continue
			}
		}
		go n.initPeer(conn, "", false)
	}
}

// gossipLoop periodically asks peers to share their peer lists, helping nodes
// that joined late discover the rest of the swarm.
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

func (n *Node) dial(addr string, requestPeerList bool) error {
	return n.dialHinted(addr, "", requestPeerList)
}

// dialHinted tries addr first (direct LAN path); if that fails and extAddr is
// known, tries the external address directly; if that also fails and we have a
// hole-punch session, performs a coordinated UDP punch and then attempts TCP
// via the opened NAT path.
func (n *Node) dialHinted(addr, extAddr string, requestPeerList bool) error {
	if n.peers.Has(addr) || (extAddr != "" && n.peers.Has(extAddr)) {
		return nil
	}

	// 1. Try the internal / direct address first.
	if conn, err := net.DialTimeout("tcp", addr, 4*time.Second); err == nil {
		go n.initPeer(conn, addr, requestPeerList)
		return nil
	}

	// 2. Try the external address directly (works when both peers are behind
	//    full-cone NATs, or when the peer IS at that address already).
	if extAddr != "" && extAddr != addr {
		if conn, err := net.DialTimeout("tcp", extAddr, 4*time.Second); err == nil {
			go n.initPeer(conn, extAddr, requestPeerList)
			return nil
		}
	}

	// 3. Hole punch via coordinated UDP probing.
	if n.hpSession == nil || extAddr == "" {
		if extAddr != "" {
			return fmt.Errorf("dial %s (ext %s): unreachable", addr, extAddr)
		}
		return fmt.Errorf("dial %s: unreachable", addr)
	}

	// 3a. Register a channel for the ack from the target.
	token := newToken()
	waitCh := make(chan protocol.HolePunchAckPayload, 1)
	n.pendingPunches.Store(token, waitCh)
	defer n.pendingPunches.Delete(token)

	reqMsg, err := protocol.NewMessage(protocol.MsgHolePunchReq, protocol.HolePunchReqPayload{
		Token:           token,
		RequesterExtUDP: n.hpSession.ExternalAddr(),
		TargetExtUDP:    extAddr,
	})
	if err != nil {
		return fmt.Errorf("build holepunch_req: %w", err)
	}

	// Broadcast to all connected peers; one of them is the relay for the target.
	n.broadcast(reqMsg)

	// 3b. Start punching immediately — don't wait for the ack before sending
	//     probes, because even a tiny head-start opens our NAT for the target.
	punchTarget := extAddr
	punchDone := make(chan error, 1)
	go func() {
		punchDone <- n.hpSession.Punch(punchTarget, 15*time.Second)
	}()

	// 3c. Wait for punch to succeed, optionally updating the target addr from ack.
	select {
	case ack := <-waitCh:
		// Target may have confirmed a corrected external addr.
		if ack.AckerExtUDP != "" && ack.AckerExtUDP != punchTarget {
			punchTarget = ack.AckerExtUDP
			fmt.Printf("[info] hole punch: target ext addr corrected to %s\n", punchTarget)
		}
	case <-time.After(10 * time.Second):
		fmt.Printf("[warn] hole punch to %s: no ack from target (continuing anyway)\n", extAddr)
	}

	// Wait for the UDP punch goroutine to complete (or time out).
	select {
	case punchErr := <-punchDone:
		if punchErr != nil {
			fmt.Printf("[warn] hole punch UDP to %s: %v\n", punchTarget, punchErr)
			// Continue — a partial punch may still allow TCP.
		} else {
			fmt.Printf("[info] hole punch to %s: UDP hole confirmed open\n", punchTarget)
		}
	case <-time.After(16 * time.Second):
		fmt.Printf("[warn] hole punch to %s: UDP punch goroutine timed out\n", punchTarget)
	}

	// 3d. TCP via the opened NAT mapping, reusing the same local port.
	conn, err := n.hpSession.DialTCP(punchTarget, 8*time.Second)
	if err != nil {
		return fmt.Errorf("hole punch to %s: TCP upgrade failed: %w", punchTarget, err)
	}
	fmt.Printf("[info] hole punch to %s: TCP connection established\n", punchTarget)
	go n.initPeer(conn, punchTarget, requestPeerList)
	return nil
}

// newToken returns a cryptographically random uint64 for hole-punch nonces.
func newToken() uint64 {
	var b [8]byte
	rand.Read(b[:]) //nolint:errcheck
	return binary.BigEndian.Uint64(b[:])
}

// initPeer drives the handshake for a newly established connection (inbound
// or outbound) and then starts the peer's read/write loops.
func (n *Node) initPeer(conn net.Conn, knownAddr string, requestPeerList bool) {
	// Derive the remote listener address from the connection or the known addr.
	remoteAddr := knownAddr
	if remoteAddr == "" {
		remoteAddr = conn.RemoteAddr().String()
	}

	p := newPeer(conn, remoteAddr)

	// --- plain-text handshake ---
	listenPort := listenPort(n.cfg.ListenAddr)
	hsMsg, err := protocol.NewMessage(protocol.MsgHandshake, protocol.HandshakePayload{
		Nick:         n.cfg.Nick,
		ListenPort:   listenPort,
		Crypto:       n.cfg.Crypto,
		ExternalAddr: n.cfg.ExternalAddr,
	})
	if err != nil || protocol.WriteMessage(conn, hsMsg) != nil {
		conn.Close()
		return
	}

	resp, err := protocol.ReadMessage(conn)
	if err != nil || resp.Type != protocol.MsgHandshake {
		conn.Close()
		return
	}
	var theirHS protocol.HandshakePayload
	if err := json.Unmarshal(resp.Payload, &theirHS); err != nil {
		conn.Close()
		return
	}
	p.Nick = theirHS.Nick
	p.ExtAddr = theirHS.ExternalAddr

	// Update the peer addr to use the peer's actual listener port.
	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	p.Addr = net.JoinHostPort(host, fmt.Sprintf("%d", theirHS.ListenPort))

	// Avoid duplicate connections.
	if n.peers.Has(p.Addr) {
		conn.Close()
		return
	}

	// --- optional crypto handshake ---
	if n.cfg.Crypto && theirHS.Crypto && n.keyPair != nil {
		cc, err := n.cryptoHandshake(conn, *n.keyPair)
		if err != nil {
			fmt.Printf("[warn] crypto handshake with %s: %v\n", p.Addr, err)
			conn.Close()
			return
		}
		p.EnableCrypto(cc)
	}

	n.peers.Add(p)
	go p.writeLoop()
	go p.readLoop(n.handleMessage)

	for _, fn := range n.onPeer {
		fn(PeerInfo{Addr: p.Addr, Nick: p.Nick, Crypto: p.HasCrypto(), ExtAddr: p.ExtAddr})
	}

	// Announce shared folders to the new peer.
	go n.sendFolderAnnounce(p)

	// Clean up when the peer disconnects.
	go func() {
		<-p.Done()
		n.peers.Remove(p.Addr)
		fmt.Printf("[info] peer %s (%s) disconnected\n", p.Nick, p.Addr)
		for _, fn := range n.onPeerLeave {
			fn(p.Addr)
		}
		for _, sf := range n.folders {
			sf.removePeer(p.Addr)
		}
	}()

	if requestPeerList {
		reqMsg, _ := protocol.NewMessage(protocol.MsgPeerListReq, struct{}{})
		p.Send(reqMsg)
	}
}

// cryptoHandshake exchanges public keys and returns a ready CryptoConn.
func (n *Node) cryptoHandshake(rw net.Conn, kp protocol.KeyPair) (*protocol.CryptoConn, error) {
	// Send our public key.
	outMsg, err := protocol.NewMessage(protocol.MsgCryptoHandshake, protocol.CryptoHandshakePayload{
		PublicKey: kp.Public,
	})
	if err != nil {
		return nil, err
	}
	if err := protocol.WriteMessage(rw, outMsg); err != nil {
		return nil, err
	}

	// Receive their public key.
	inMsg, err := protocol.ReadMessage(rw)
	if err != nil {
		return nil, err
	}
	if inMsg.Type != protocol.MsgCryptoHandshake {
		return nil, fmt.Errorf("expected crypto_handshake, got %s", inMsg.Type)
	}
	var theirKey protocol.CryptoHandshakePayload
	if err := json.Unmarshal(inMsg.Payload, &theirKey); err != nil {
		return nil, err
	}

	return protocol.NewCryptoConn(rw, kp.Private, theirKey.PublicKey), nil
}

// handleMessage dispatches an incoming message to the appropriate handler.
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
		n.recv.HandleChunk(msg.Payload)
	case protocol.MsgPeerListReq:
		n.handlePeerListReq(p)
	case protocol.MsgPeerListRes:
		n.handlePeerListRes(p, msg)
	case protocol.MsgHolePunchReq:
		n.handleHolePunchReq(p, msg)
	case protocol.MsgHolePunchAck:
		n.handleHolePunchAck(p, msg)
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

// handleHolePunchReq processes an inbound hole-punch request.
//
//   - If this node is the target (our ext UDP addr matches TargetExtUDP),
//     we start punching back and broadcast an ack so relays can route it
//     to the requester.
//   - Otherwise, we act as a relay: if we have a peer whose ExtAddr matches
//     TargetExtUDP, we forward the request to that peer (one hop only).
func (n *Node) handleHolePunchReq(from *Peer, msg protocol.Message) {
	var req protocol.HolePunchReqPayload
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return
	}
	if n.hpSession == nil {
		return
	}

	if n.hpSession.ExternalAddr() == req.TargetExtUDP {
		// We are the target.  Punch back toward the requester.
		ack, err := protocol.NewMessage(protocol.MsgHolePunchAck, protocol.HolePunchAckPayload{
			Token:           req.Token,
			AckerExtUDP:     n.hpSession.ExternalAddr(),
			RequesterExtUDP: req.RequesterExtUDP,
		})
		if err == nil {
			n.broadcast(ack)
		}
		if req.RequesterExtUDP != "" {
			go func() {
				if err := n.hpSession.Punch(req.RequesterExtUDP, 15*time.Second); err != nil {
					fmt.Printf("[warn] hole punch to requester %s: %v\n", req.RequesterExtUDP, err)
				}
			}()
		}
		return
	}

	// We are a relay.  Forward to the peer that has the matching ext addr.
	if target := n.findPeerByExtAddr(req.TargetExtUDP); target != nil && target.Addr != from.Addr {
		target.Send(msg)
	}
}

// handleHolePunchAck processes an inbound hole-punch ack.
//
//   - If this node is the requester (our ext UDP addr matches RequesterExtUDP),
//     deliver the ack to the waiting dialHinted goroutine.
//   - Otherwise act as a relay: forward to the peer whose ExtAddr matches
//     RequesterExtUDP.
func (n *Node) handleHolePunchAck(from *Peer, msg protocol.Message) {
	var ack protocol.HolePunchAckPayload
	if err := json.Unmarshal(msg.Payload, &ack); err != nil {
		return
	}

	if n.hpSession != nil && n.hpSession.ExternalAddr() == ack.RequesterExtUDP {
		// We are the requester.  Deliver to the waiting channel.
		if ch, ok := n.pendingPunches.Load(ack.Token); ok {
			select {
			case ch.(chan protocol.HolePunchAckPayload) <- ack:
			default:
			}
		}
		return
	}

	// Relay: forward to the peer who has the matching ext addr.
	if requester := n.findPeerByExtAddr(ack.RequesterExtUDP); requester != nil && requester.Addr != from.Addr {
		requester.Send(msg)
	}
}

// findPeerByExtAddr returns the first peer whose ExtAddr equals extAddr,
// or nil if none is found.
func (n *Node) findPeerByExtAddr(extAddr string) *Peer {
	if extAddr == "" {
		return nil
	}
	for _, p := range n.peers.List() {
		if p.ExtAddr == extAddr {
			return p
		}
	}
	return nil
}

func (n *Node) handlePeerListReq(requester *Peer) {
	peerList := n.peers.List()
	filtered := make([]string, 0, len(peerList))
	ext := make(map[string]string)
	for _, p := range peerList {
		if p.Addr != requester.Addr {
			filtered = append(filtered, p.Addr)
			if p.ExtAddr != "" {
				ext[p.Addr] = p.ExtAddr
			}
		}
	}
	payload := protocol.PeerListPayload{Addrs: filtered}
	if len(ext) > 0 {
		payload.Ext = ext
	}
	resp, err := protocol.NewMessage(protocol.MsgPeerListRes, payload)
	if err != nil {
		return
	}
	requester.Send(resp)
}

func (n *Node) handlePeerListRes(p *Peer, msg protocol.Message) {
	var pl protocol.PeerListPayload
	if err := json.Unmarshal(msg.Payload, &pl); err != nil {
		return
	}
	for _, addr := range pl.Addrs {
		extAddr := ""
		if pl.Ext != nil {
			extAddr = pl.Ext[addr]
		}
		if !n.peers.Has(addr) && (extAddr == "" || !n.peers.Has(extAddr)) {
			go func(a, ext string) {
				if err := n.dialHinted(a, ext, false); err != nil {
					fmt.Printf("[info] could not connect to discovered peer %s: %v\n", a, err)
				}
			}(addr, extAddr)
		}
	}
}

func (n *Node) broadcast(msg protocol.Message) {
	for _, p := range n.peers.List() {
		p.Send(msg)
	}
}

// listenPort extracts the port number from a listen address like ":9000".
func listenPort(addr string) int {
	parts := strings.Split(addr, ":")
	if len(parts) == 0 {
		return 0
	}
	port := 0
	fmt.Sscanf(parts[len(parts)-1], "%d", &port)
	return port
}
