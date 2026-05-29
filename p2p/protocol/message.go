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
	MsgFileChecksum    = "file_checksum" // trailer: SHA-256 of preceding chunks
	MsgFileAck         = "file_ack"      // receiver → sender: file ok / failed
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

// Message is the top-level wire format: a type tag, a JSON payload, and an
// optional binary trailer that is NOT JSON-encoded. The trailer lets us avoid
// base64 inflation on large blobs like file chunks.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	Bin     []byte          `json:"-"` // raw binary; goes in the wire trailer
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

// FileMetaPayload introduces a file_meta message. Checksum may be empty when
// the sender is computing it streaming; in that case the receiver waits for a
// MsgFileChecksum trailer keyed by ID before verifying and finalising.
type FileMetaPayload struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum,omitempty"` // hex-encoded SHA-256; optional
}

// FileChunkPayload carries chunk metadata only; the raw bytes ride in
// Message.Bin to avoid base64 overhead.
type FileChunkPayload struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
	Final bool   `json:"final"`
}

// FileChecksumPayload is sent after the final chunk when the sender computed
// the hash streaming. The receiver verifies it against its accumulated hash.
type FileChecksumPayload struct {
	ID       string `json:"id"`
	Checksum string `json:"checksum"` // hex-encoded SHA-256
}

// FileAckPayload is the receiver's positive (or negative) confirmation that a
// file arrived intact. Sent after checksum verification on the receive side.
type FileAckPayload struct {
	ID  string `json:"id"`
	OK  bool   `json:"ok"`
	Err string `json:"err,omitempty"`
}

// --- Shared folder payloads ---

// FolderAnnouncePayload advertises which shared folders this peer exposes.
type FolderAnnouncePayload struct {
	Names []string `json:"names"`
}

// FolderFileMetaPayload precedes chunks for a shared-folder file.
// Chunks arrive as ordinary MsgFileChunk messages keyed by ID. Checksum may be
// empty (see FileMetaPayload notes).
type FolderFileMetaPayload struct {
	Folder   string `json:"folder"`
	ID       string `json:"id"`
	RelPath  string `json:"rel_path"` // forward-slash relative path inside the folder
	Size     int64  `json:"size"`
	Checksum string `json:"checksum,omitempty"` // hex-encoded SHA-256; optional
	ModTime  int64  `json:"mod_time"`           // unix seconds — used for last-write-wins
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

// NewMessageBin is like NewMessage but attaches a binary trailer that is sent
// raw on the wire (no base64).
func NewMessageBin(msgType string, payload interface{}, bin []byte) (Message, error) {
	msg, err := NewMessage(msgType, payload)
	if err != nil {
		return Message{}, err
	}
	msg.Bin = bin
	return msg, nil
}

// NewChatMessage is a convenience for chat messages.
func NewChatMessage(nick, text string) (Message, error) {
	return NewMessage(MsgChat, ChatPayload{
		Nick: nick,
		Text: text,
		Time: time.Now().Unix(),
	})
}

// --- Wire encoding ---
//
// Wire format:
//   [4-byte big-endian json_len]
//   [4-byte big-endian bin_len]
//   [json_len bytes of JSON-encoded Message (without Bin)]
//   [bin_len bytes of raw binary trailer]
//
// Either length may be zero. Total wire size is 8 + json_len + bin_len.

const maxMessageSize = 128 * 1024 * 1024 // 128 MB per side (chunks are 1 MB)

// WriteMessage writes a length-prefixed binary-friendly message to w.
// Callers should pass a buffered writer when sending many small messages.
func WriteMessage(w io.Writer, msg Message) error {
	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(len(jsonBytes)))
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(msg.Bin)))
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := w.Write(jsonBytes); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	if len(msg.Bin) > 0 {
		if _, err := w.Write(msg.Bin); err != nil {
			return fmt.Errorf("write bin: %w", err)
		}
	}
	return nil
}

// ReadMessage reads one wire message from r.
func ReadMessage(r io.Reader) (Message, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Message{}, err
	}
	jsonLen := binary.BigEndian.Uint32(hdr[0:4])
	binLen := binary.BigEndian.Uint32(hdr[4:8])
	if jsonLen > maxMessageSize || binLen > maxMessageSize {
		return Message{}, fmt.Errorf("message too large: json=%d bin=%d", jsonLen, binLen)
	}
	jsonBytes := make([]byte, jsonLen)
	if _, err := io.ReadFull(r, jsonBytes); err != nil {
		return Message{}, fmt.Errorf("read json: %w", err)
	}
	var msg Message
	if err := json.Unmarshal(jsonBytes, &msg); err != nil {
		return Message{}, fmt.Errorf("unmarshal: %w", err)
	}
	if binLen > 0 {
		msg.Bin = make([]byte, binLen)
		if _, err := io.ReadFull(r, msg.Bin); err != nil {
			return Message{}, fmt.Errorf("read bin: %w", err)
		}
	}
	return msg, nil
}
