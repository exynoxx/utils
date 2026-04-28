package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Message types
const (
	MsgChat            = "chat"
	MsgFileMeta        = "file_meta"
	MsgFileChunk       = "file_chunk"
	MsgPeerListReq     = "peer_list_req"
	MsgPeerListRes     = "peer_list_res"
	MsgHandshake       = "handshake"
	MsgCryptoHandshake = "crypto_handshake"
	// NAT hole-punch coordination (relayed through existing connections)
	MsgHolePunchReq = "holepunch_req"
	MsgHolePunchAck = "holepunch_ack"
	// Shared folder sync
	MsgFolderAnnounce = "folder_announce"
	MsgFolderFileMeta = "folder_file_meta"
	MsgFolderDelete   = "folder_delete"
)

// Message is the top-level wire format: a type tag + raw JSON payload.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// --- Payload types ---

type ChatPayload struct {
	Nick string `json:"nick"`
	Text string `json:"text"`
	Time int64  `json:"time"`
}

type HandshakePayload struct {
	Nick         string `json:"nick"`
	ListenPort   int    `json:"listen_port"`
	Crypto       bool   `json:"crypto"`
	ExternalAddr string `json:"external_addr,omitempty"`
}

type CryptoHandshakePayload struct {
	PublicKey [32]byte `json:"public_key"`
}

type PeerListPayload struct {
	Addrs []string          `json:"addrs"`
	Ext   map[string]string `json:"ext,omitempty"` // addr → external addr
}

// HolePunchReqPayload is broadcast through relay peers to initiate a
// coordinated UDP hole-punch.  The target peer is identified by TargetExtUDP.
type HolePunchReqPayload struct {
	Token           uint64 `json:"token"`             // random nonce; correlates req ↔ ack
	RequesterExtUDP string `json:"requester_ext_udp"` // requester's external UDP addr
	TargetExtUDP    string `json:"target_ext_udp"`    // target's external UDP addr
}

// HolePunchAckPayload is sent back (via relay) when the target has received
// the request and begun punching.  Relays forward it based on RequesterExtUDP.
type HolePunchAckPayload struct {
	Token           uint64 `json:"token"`
	AckerExtUDP     string `json:"acker_ext_udp"`     // target's confirmed external UDP addr
	RequesterExtUDP string `json:"requester_ext_udp"` // used by relays to route the ack back
}

type FileMetaPayload struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"` // hex-encoded SHA-256
}

type FileChunkPayload struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
	Data  []byte `json:"data"`
	Final bool   `json:"final"`
}

// --- Shared folder payloads ---

// FolderAnnouncePayload advertises which shared folders this peer exposes.
type FolderAnnouncePayload struct {
	Names []string `json:"names"`
}

// FolderFileMetaPayload precedes chunks for a shared-folder file.
// Chunks arrive as ordinary MsgFileChunk messages keyed by ID.
type FolderFileMetaPayload struct {
	Folder   string `json:"folder"`
	ID       string `json:"id"`
	RelPath  string `json:"rel_path"`  // forward-slash relative path inside the folder
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`  // hex-encoded SHA-256
	ModTime  int64  `json:"mod_time"`  // unix seconds — used for last-write-wins
}

// FolderDeletePayload signals that a file was removed from a shared folder.
type FolderDeletePayload struct {
	Folder  string `json:"folder"`
	RelPath string `json:"rel_path"`
}

// NewMessage builds a Message from a type and any payload struct.
func NewMessage(msgType string, payload interface{}) (Message, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Message{}, fmt.Errorf("marshal payload: %w", err)
	}
	return Message{Type: msgType, Payload: raw}, nil
}

// NewChatMessage is a convenience for chat messages.
func NewChatMessage(nick, text string) (Message, error) {
	return NewMessage(MsgChat, ChatPayload{
		Nick: nick,
		Text: text,
		Time: time.Now().Unix(),
	})
}

// --- Wire encoding: 4-byte big-endian length prefix + JSON body ---

const maxMessageSize = 128 * 1024 * 1024 // 128 MB (generous for file chunks)

// WriteMessage writes a length-prefixed JSON message to w.
func WriteMessage(w io.Writer, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
}

// ReadMessage reads a length-prefixed JSON message from r.
func ReadMessage(r io.Reader) (Message, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return Message{}, err
	}
	if length > maxMessageSize {
		return Message{}, fmt.Errorf("message too large: %d bytes", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return Message{}, fmt.Errorf("read body: %w", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, fmt.Errorf("unmarshal message: %w", err)
	}
	return msg, nil
}
