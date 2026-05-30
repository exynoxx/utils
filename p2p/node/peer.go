package node

import (
	"bufio"
	"fmt"
	"sync"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"p2p/protocol"
)

// Peer represents a single connected remote peer. The underlying transport is
// a libp2p stream on the app protocol; libp2p already authenticates and
// encrypts the connection, so this layer only deals with message framing.
type Peer struct {
	stream  network.Stream
	Addr    string // canonical identifier: the remote peer.ID string
	pid     peer.ID
	Nick    string
	writeCh chan protocol.Message
	once    sync.Once // ensures Close is idempotent
	done    chan struct{}
}

func newPeer(s network.Stream, pid peer.ID) *Peer {
	return &Peer{
		stream:  s,
		Addr:    pid.String(),
		pid:     pid,
		writeCh: make(chan protocol.Message, 64),
		done:    make(chan struct{}),
	}
}

// Send enqueues a message for delivery to this peer.
// Blocks until the message is queued or the peer is closed.
func (p *Peer) Send(msg protocol.Message) {
	select {
	case p.writeCh <- msg:
	case <-p.done:
	}
}

// Close shuts down the peer's stream in both directions. Idempotent.
func (p *Peer) Close() {
	p.once.Do(func() {
		close(p.done)
		_ = p.stream.Reset()
	})
}

// Done returns a channel that is closed when the peer is disconnected.
func (p *Peer) Done() <-chan struct{} { return p.done }

// writeLoop drains writeCh and frames messages onto the stream.
//
// A large bufio.Writer (1 MB) coalesces back-to-back small messages into one
// write; we only Flush when writeCh is drained so 1 MB file chunks don't
// fragment into tiny writes.
func (p *Peer) writeLoop() {
	const writeBufSize = 1024 * 1024
	w := bufio.NewWriterSize(p.stream, writeBufSize)
	for {
		select {
		case <-p.done:
			return
		case msg, ok := <-p.writeCh:
			if !ok {
				return
			}
			if err := protocol.WriteMessage(w, msg); err != nil {
				p.Close()
				return
			}
			// Only flush when no more work is queued, so chunks coalesce.
			if len(p.writeCh) == 0 {
				if err := w.Flush(); err != nil {
					p.Close()
					return
				}
			}
		}
	}
}

// readLoop reads framed messages from the stream and dispatches them via
// handler. Runs until the stream is closed or reset.
func (p *Peer) readLoop(handler func(*Peer, protocol.Message)) {
	defer p.Close()
	r := bufio.NewReader(p.stream)
	for {
		msg, err := protocol.ReadMessage(r)
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
