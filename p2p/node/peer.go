package node

import (
	"bufio"
	"fmt"
	"net"
	"sync"

	"p2p/protocol"
)

// Peer represents a single connected remote peer.
type Peer struct {
	Conn       net.Conn
	Addr       string // canonical "host:port" of the peer's listener
	Nick       string
	ExtAddr    string // external (internet-facing) addr from STUN, may be empty
	writeCh    chan protocol.Message
	crypto     *protocol.CryptoConn // non-nil when encryption is active
	once       sync.Once            // ensures close is idempotent
	done       chan struct{}
}

func newPeer(conn net.Conn, addr string) *Peer {
	return &Peer{
		Conn:    conn,
		Addr:    addr,
		writeCh: make(chan protocol.Message, 64),
		done:    make(chan struct{}),
	}
}

// EnableCrypto attaches a CryptoConn to this peer. Must be called before
// the read/write loops are started (i.e. during handshake).
func (p *Peer) EnableCrypto(cc *protocol.CryptoConn) {
	p.crypto = cc
}

// Send enqueues a message for delivery to this peer.
// Blocks until the message is queued or the peer is closed.
func (p *Peer) Send(msg protocol.Message) {
	select {
	case p.writeCh <- msg:
	case <-p.done:
	}
}

// Close shuts down the peer connection.
func (p *Peer) Close() {
	p.once.Do(func() {
		close(p.done)
		p.Conn.Close()
	})
}

// Done returns a channel that is closed when the peer is disconnected.
func (p *Peer) Done() <-chan struct{} {
	return p.done
}

// HasCrypto reports whether NaCl encryption is active for this peer.
func (p *Peer) HasCrypto() bool { return p.crypto != nil }

// writeLoop drains writeCh and sends messages to the peer.
func (p *Peer) writeLoop() {
	w := bufio.NewWriter(p.Conn)
	for {
		select {
		case <-p.done:
			return
		case msg, ok := <-p.writeCh:
			if !ok {
				return
			}
			var err error
			if p.crypto != nil {
				err = p.crypto.WriteMessage(msg)
			} else {
				err = protocol.WriteMessage(w, msg)
				if err == nil {
					err = w.Flush()
				}
			}
			if err != nil {
				p.Close()
				return
			}
		}
	}
}

// readLoop reads messages from the peer and dispatches them via handler.
// Runs until the connection is closed.
func (p *Peer) readLoop(handler func(*Peer, protocol.Message)) {
	defer p.Close()
	r := bufio.NewReader(p.Conn)
	for {
		var (
			msg protocol.Message
			err error
		)
		if p.crypto != nil {
			msg, err = p.crypto.ReadMessage()
		} else {
			msg, err = protocol.ReadMessage(r)
		}
		if err != nil {
			select {
			case <-p.done:
				// expected close
			default:
				fmt.Printf("[warn] peer %s read error: %v\n", p.Addr, err)
			}
			return
		}
		handler(p, msg)
	}
}
