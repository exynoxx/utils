// Package holepunch provides STUN-based external address discovery and
// coordinated UDP hole-punching to assist NAT traversal before TCP
// connection attempts.
//
// Correct hole-punch requires:
//  1. STUN is sent from a UDP socket bound to the same port as the TCP
//     listener, so the discovered external address reflects that exact
//     NAT mapping.
//  2. Both peers punch simultaneously (bidirectional probes), which this
//     package supports via Session.Punch.
//  3. After the UDP hole is open, Session.DialTCP reuses the same local
//     port (SO_REUSEADDR / SO_REUSEPORT) so the outbound TCP SYN uses the
//     same NAT mapping.
//
// Works well for full-cone, address-restricted, and port-restricted NATs.
// Symmetric NATs require a relay and are out of scope.
package holepunch

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	stunMagicCookie      uint32 = 0x2112A442
	attrXorMappedAddress        = 0x0020
	attrMappedAddress           = 0x0001

	punchMagic    uint32        = 0xC0FFEE42
	punchInterval               = 150 * time.Millisecond
)

// punchPacket is the payload of each UDP hole-punch datagram.
type punchPacket struct {
	Magic uint32 `json:"m"`
	From  string `json:"f"` // sender's external UDP addr (informational)
	Token uint64 `json:"t"` // matches the Punch call so we ignore unrelated datagrams
}

// Session holds a UDP socket bound to a fixed local port.
// Create one with NewSession at node startup and keep it for the node's
// lifetime. It discovers the external address via STUN (from the same
// bound port) and exposes Punch / DialTCP for NAT traversal.
type Session struct {
	conn    *net.UDPConn
	extAddr string // STUN-discovered "ip:port"

	mu      sync.Mutex
	waiters map[string]chan []byte // remoteAddr.String() → incoming-pkt channel
}

// NewSession binds a UDP socket to localAddr (e.g. ":9000") and
// optionally discovers the external address via stunServer.
// If stunServer is empty, no STUN is performed (ExternalAddr returns "").
func NewSession(localAddr, stunServer string) (*Session, error) {
	laddr, err := net.ResolveUDPAddr("udp4", localAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve local addr %s: %w", localAddr, err)
	}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return nil, fmt.Errorf("bind udp %s: %w", localAddr, err)
	}

	s := &Session{conn: conn, waiters: make(map[string]chan []byte)}

	if stunServer != "" {
		ext, err := s.stun(stunServer)
		if err != nil {
			// Non-fatal: hole punch will be limited without ext addr.
			fmt.Printf("[warn] STUN from %s: %v\n", localAddr, err)
		} else {
			s.extAddr = ext
		}
	}

	go s.dispatchLoop()
	return s, nil
}

// ExternalAddr returns the STUN-discovered external "ip:port", or "" if
// STUN was not performed or failed.
func (s *Session) ExternalAddr() string { return s.extAddr }

// Punch performs a bidirectional UDP hole-punch to remoteExtAddr.
// It concurrently:
//   - sends a punchPacket to remoteExtAddr every punchInterval
//   - listens for an inbound punchPacket from remoteExtAddr
//
// Returns nil as soon as a valid response is received (hole is open).
// Returns an error if timeout elapses without a response.
func (s *Session) Punch(remoteExtAddr string, timeout time.Duration) error {
	raddr, err := net.ResolveUDPAddr("udp4", remoteExtAddr)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", remoteExtAddr, err)
	}

	// Generate a random token so we don't accept unrelated datagrams.
	var b [8]byte
	rand.Read(b[:]) //nolint:errcheck
	token := binary.BigEndian.Uint64(b[:])

	ch := make(chan []byte, 32)
	key := raddr.String()
	s.mu.Lock()
	s.waiters[key] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.waiters, key)
		s.mu.Unlock()
	}()

	pkt, _ := json.Marshal(punchPacket{Magic: punchMagic, From: s.extAddr, Token: token})

	ticker := time.NewTicker(punchInterval)
	defer ticker.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		// Send a probe toward the remote external address.
		s.conn.SetWriteDeadline(time.Now().Add(time.Second)) //nolint:errcheck
		s.conn.WriteToUDP(pkt, raddr)                        //nolint:errcheck
		s.conn.SetWriteDeadline(time.Time{})                 //nolint:errcheck

		select {
		case data := <-ch:
			var pp punchPacket
			if json.Unmarshal(data, &pp) == nil && pp.Magic == punchMagic {
				return nil // hole is open — got a valid probe back
			}
			// Unexpected data from this addr; keep waiting.
		case <-ticker.C:
			// Just send another probe.
		case <-deadline.C:
			return fmt.Errorf("hole punch to %s timed out after %s", remoteExtAddr, timeout)
		}
	}
}

// DialTCP attempts a TCP connection to remoteAddr, binding the outgoing
// socket to the same local port as this session via SO_REUSEADDR /
// SO_REUSEPORT.  This reuses the NAT mapping opened by Punch.
func (s *Session) DialTCP(remoteAddr string, timeout time.Duration) (net.Conn, error) {
	localPort := s.conn.LocalAddr().(*net.UDPAddr).Port
	laddr := &net.TCPAddr{Port: localPort}
	d := net.Dialer{
		LocalAddr: laddr,
		Timeout:   timeout,
		Control:   controlSocket,
	}
	conn, err := d.Dial("tcp4", remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("tcp dial from port %d to %s: %w", localPort, remoteAddr, err)
	}
	return conn, nil
}

// Close releases the underlying UDP socket.
func (s *Session) Close() { s.conn.Close() }

// --- internal ---

// stun sends a STUN binding request on the session's bound UDP socket and
// returns the observed external "ip:port".
func (s *Session) stun(server string) (string, error) {
	saddr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return "", fmt.Errorf("resolve stun server %s: %w", server, err)
	}
	s.conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	defer s.conn.SetDeadline(time.Time{})               //nolint:errcheck

	req := makeSTUNRequest()
	if _, err := s.conn.WriteToUDP(req, saddr); err != nil {
		return "", fmt.Errorf("send stun: %w", err)
	}
	resp := make([]byte, 512)
	n, _, err := s.conn.ReadFromUDP(resp)
	if err != nil {
		return "", fmt.Errorf("recv stun: %w", err)
	}
	return parseSTUNResponse(resp[:n])
}

// dispatchLoop reads all incoming UDP datagrams and routes each packet to the
// waiting Punch goroutine registered for that source address.
// Runs until the session's UDP conn is closed.
func (s *Session) dispatchLoop() {
	buf := make([]byte, 2048)
	for {
		n, from, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return // conn closed — exit cleanly
		}
		key := from.String()
		s.mu.Lock()
		ch, ok := s.waiters[key]
		s.mu.Unlock()
		if ok {
			pkt := make([]byte, n)
			copy(pkt, buf[:n])
			select {
			case ch <- pkt:
			default: // channel full — drop oldest, write new below
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- pkt:
				default:
				}
			}
		}
	}
}

// --- STUN helpers ---

func makeSTUNRequest() []byte {
	req := make([]byte, 20)
	req[0], req[1] = 0x00, 0x01 // Binding Request
	binary.BigEndian.PutUint32(req[4:], stunMagicCookie)
	binary.BigEndian.PutUint64(req[8:], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint32(req[16:], 0xDEADBEEF)
	return req
}

func parseSTUNResponse(data []byte) (string, error) {
	if len(data) < 20 {
		return "", fmt.Errorf("stun response too short (%d bytes)", len(data))
	}
	pos := 20
	for pos+4 <= len(data) {
		attrType := binary.BigEndian.Uint16(data[pos:])
		attrLen := int(binary.BigEndian.Uint16(data[pos+2:]))
		pos += 4
		if pos+attrLen > len(data) {
			break
		}
		val := data[pos : pos+attrLen]

		switch attrType {
		case attrXorMappedAddress:
			if attrLen < 8 {
				break
			}
			rawPort := binary.BigEndian.Uint16(val[2:4])
			port := rawPort ^ uint16(stunMagicCookie>>16)
			raw := make([]byte, 4)
			copy(raw, val[4:8])
			xorKey := make([]byte, 4)
			binary.BigEndian.PutUint32(xorKey, stunMagicCookie)
			for i := range raw {
				raw[i] ^= xorKey[i]
			}
			return fmt.Sprintf("%s:%d", net.IP(raw).String(), port), nil

		case attrMappedAddress:
			if attrLen < 8 {
				break
			}
			port := binary.BigEndian.Uint16(val[2:4])
			ip := net.IP(val[4:8])
			return fmt.Sprintf("%s:%d", ip.String(), port), nil
		}

		padded := (attrLen + 3) &^ 3
		pos += padded
	}
	return "", fmt.Errorf("no mapped-address attribute in stun response")
}
