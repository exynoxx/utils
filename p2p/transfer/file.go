// Package transfer handles chunked file sending and receiving over the P2P
// message protocol. It is intentionally decoupled from network I/O — callers
// supply a send function so the package has no dependency on Node internals.
package transfer

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"p2p/protocol"
)

const chunkSize = 1024 * 1024 // 1 MB per chunk

// chunkPool reuses 1 MB scratch buffers so we don't allocate one per chunk.
// Note: the buffer is consumed synchronously by sendFn (which serialises into
// its own buffer in WriteMessage), so it's safe to put back when sendFn returns.
var chunkPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, chunkSize)
		return &b
	},
}

// SendFunc is the signature for enqueuing a message to a specific peer.
type SendFunc func(msg protocol.Message)

// ProgressFunc is called periodically during transfers with the file name,
// bytes transferred so far, total size, and the remote peer address. It may
// be nil.
type ProgressFunc func(name string, sent, total int64, peerAddr string)

// Send streams the file at path to a peer.  It announces the file with
// MsgFileMeta, sends MsgFileChunk messages (raw bytes in Message.Bin — no
// base64), and emits a MsgFileChecksum trailer.  The file is read exactly
// once; SHA-256 is computed while streaming.
//
// progressFn (may be nil) is called per chunk; peerAddr is forwarded so
// callers can attribute progress events to a peer.
func Send(path, peerAddr string, sendFn SendFunc, progressFn ProgressFunc) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	id := generateID()
	metaMsg, err := protocol.NewMessage(protocol.MsgFileMeta, protocol.FileMetaPayload{
		ID:   id,
		Name: filepath.Base(path),
		Size: info.Size(),
		// Checksum sent as a trailer (MsgFileChecksum) — we compute it streaming.
	})
	if err != nil {
		return fmt.Errorf("build file_meta: %w", err)
	}
	sendFn(metaMsg)

	checksum, err := streamChunks(f, id, info.Size(), filepath.Base(path), peerAddr, sendFn, progressFn)
	if err != nil {
		return err
	}

	trailer, err := protocol.NewMessage(protocol.MsgFileChecksum, protocol.FileChecksumPayload{
		ID:       id,
		Checksum: checksum,
	})
	if err != nil {
		return fmt.Errorf("build file_checksum: %w", err)
	}
	sendFn(trailer)
	return nil
}

// streamChunks reads f sequentially, dispatching MsgFileChunk messages with
// raw bytes in Message.Bin while updating a running SHA-256 over the stream.
// Returns the hex-encoded final hash.
func streamChunks(f *os.File, id string, total int64, displayName, peerAddr string, sendFn SendFunc, progressFn ProgressFunc) (string, error) {
	h := sha256.New()
	bufPtr := chunkPool.Get().(*[]byte)
	defer chunkPool.Put(bufPtr)
	buf := *bufPtr

	index := 0
	var sent int64
	// Zero-byte files: emit one empty final chunk so the receiver sees Final
	// and can close + finalise its session. Without this, the receive-side
	// file handle would leak.
	if total == 0 {
		chunkMsg, err := protocol.NewMessageBin(protocol.MsgFileChunk, protocol.FileChunkPayload{
			ID: id, Index: 0, Final: true,
		}, nil)
		if err != nil {
			return "", fmt.Errorf("build empty final chunk: %w", err)
		}
		sendFn(chunkMsg)
		if progressFn != nil {
			progressFn(displayName, 0, 0, peerAddr)
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			sent += int64(n)
			final := readErr == io.EOF || sent >= total
			// Hash from the same bytes we're sending — single read.
			h.Write(buf[:n])

			// Copy the chunk we hand to sendFn so the next loop iteration can
			// safely overwrite buf. sendFn is asynchronous (queues to writeCh).
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			chunkMsg, err := protocol.NewMessageBin(protocol.MsgFileChunk, protocol.FileChunkPayload{
				ID:    id,
				Index: index,
				Final: final,
			}, chunk)
			if err != nil {
				return "", fmt.Errorf("build file_chunk: %w", err)
			}
			sendFn(chunkMsg)

			if progressFn != nil {
				progressFn(displayName, sent, total, peerAddr)
			}
			index++
			if final {
				break
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", fmt.Errorf("read file: %w", readErr)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- Receiver ---

// session tracks an in-progress file receive.
// Chunks are streamed directly to disk as they arrive (TCP guarantees order),
// so memory usage is bounded to one chunk at a time regardless of file size.
type session struct {
	meta         protocol.FileMetaPayload
	file         *os.File  // open for writing; closed when Final chunk processed
	hash         hash.Hash // running sha256 updated per chunk
	received     int64
	outPath      string // final destination path
	peerAddr     string // sender address, surfaced via progressFn
	mu           sync.Mutex
	gotFinal     bool                          // last chunk processed; awaiting checksum trailer
	onComplete   func(string)                  // for plain files
	onFolderDone func(folder, rel, abs string) // for folder files
	folderName   string                        // non-empty when this is a folder transfer
	folderRel    string                        // forward-slash relPath inside folder
	progressFn   ProgressFunc                  // may be nil
}

// AckSender is the signature for sending an ACK back to the file's sender.
type AckSender func(peerAddr string, msg protocol.Message)

// Receiver collects incoming file_meta, file_chunk, and file_checksum messages
// and writes completed transfers to outDir. It can send ACKs back via ackSend.
type Receiver struct {
	outDir     string
	mu         sync.Mutex
	sessions   map[string]*session
	progressFn ProgressFunc // applied to all new receive sessions; may be nil
	ackSend    AckSender    // may be nil
}

// NewReceiver creates a Receiver that writes completed files to outDir.
func NewReceiver(outDir string) *Receiver {
	return &Receiver{
		outDir:   outDir,
		sessions: make(map[string]*session),
	}
}

// SetProgressFunc registers a callback that is invoked on every received chunk
// with the current receive percentage (0–100).  Must be called before any
// transfers begin (not thread-safe with active sessions).
func (r *Receiver) SetProgressFunc(fn ProgressFunc) { r.progressFn = fn }

// SetAckSender wires up the function used to send file_ack messages back to
// the sender after checksum verification.
func (r *Receiver) SetAckSender(fn AckSender) { r.ackSend = fn }

// HandleMeta processes a file_meta payload (raw JSON). peerAddr identifies
// the sender so the receive-side progress callback can attribute bytes to a
// peer. onComplete is called with the final file path once all chunks have
// arrived and the checksum has been verified.
func (r *Receiver) HandleMeta(raw json.RawMessage, peerAddr string, onComplete func(path string)) {
	var meta protocol.FileMetaPayload
	if err := json.Unmarshal(raw, &meta); err != nil {
		fmt.Printf("[warn] bad file_meta: %v\n", err)
		return
	}

	outPath := filepath.Join(r.outDir, filepath.Base(meta.Name))
	if err := os.MkdirAll(r.outDir, 0o750); err != nil {
		fmt.Printf("[error] create downloads dir: %v\n", err)
		return
	}
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Printf("[error] open output file %s: %v\n", outPath, err)
		return
	}

	r.mu.Lock()
	r.sessions[meta.ID] = &session{
		meta:       meta,
		file:       f,
		hash:       sha256.New(),
		outPath:    outPath,
		peerAddr:   peerAddr,
		onComplete: onComplete,
		progressFn: r.progressFn,
	}
	r.mu.Unlock()

	fmt.Printf("[recv] incoming file: %s (%d bytes)\n", meta.Name, meta.Size)
}

// HandleChunk processes a file_chunk payload (raw JSON header) plus the raw
// bytes carried in msg.Bin. Each chunk is written directly to disk; no
// full-file buffering occurs.
func (r *Receiver) HandleChunk(raw json.RawMessage, bin []byte) {
	var chunk protocol.FileChunkPayload
	if err := json.Unmarshal(raw, &chunk); err != nil {
		fmt.Printf("[warn] bad file_chunk: %v\n", err)
		return
	}

	r.mu.Lock()
	sess, ok := r.sessions[chunk.ID]
	r.mu.Unlock()
	if !ok {
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if _, err := sess.file.Write(bin); err != nil {
		fmt.Printf("[error] write chunk for %s: %v\n", sess.meta.Name, err)
		r.abortSession(chunk.ID, sess, "write error")
		return
	}
	sess.hash.Write(bin)
	sess.received += int64(len(bin))

	var pct float64
	if sess.meta.Size > 0 {
		pct = float64(sess.received) / float64(sess.meta.Size) * 100
	}
	fmt.Printf("\r[recv] %s  %.1f%%", sess.meta.Name, pct)
	if sess.progressFn != nil {
		sess.progressFn(sess.meta.Name, sess.received, sess.meta.Size, sess.peerAddr)
	}

	if chunk.Final {
		fmt.Println()
		// Close the file but keep the session alive until the checksum
		// trailer arrives; finalisation happens in HandleChecksum.
		if err := sess.file.Close(); err != nil {
			fmt.Printf("[error] close output file %s: %v\n", sess.outPath, err)
			r.abortSession(chunk.ID, sess, "close error")
			return
		}
		sess.file = nil
		sess.gotFinal = true
		// If the sender embedded a checksum in the meta (legacy), verify now.
		if sess.meta.Checksum != "" {
			r.finalize(chunk.ID, sess, sess.meta.Checksum)
		}
	}
}

// HandleChecksum processes a file_checksum trailer message. It verifies the
// running hash against the trailer, finalises the session, and sends an ACK.
func (r *Receiver) HandleChecksum(raw json.RawMessage) {
	var tr protocol.FileChecksumPayload
	if err := json.Unmarshal(raw, &tr); err != nil {
		fmt.Printf("[warn] bad file_checksum: %v\n", err)
		return
	}
	r.mu.Lock()
	sess, ok := r.sessions[tr.ID]
	r.mu.Unlock()
	if !ok {
		// Either already finalised or unknown session.
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if !sess.gotFinal {
		// Trailer arrived before final chunk somehow — should not happen with
		// in-order TCP, but record the expected checksum and bail.
		sess.meta.Checksum = tr.Checksum
		return
	}
	r.finalize(tr.ID, sess, tr.Checksum)
}

// finalize must be called with sess.mu held. It removes the session from the
// registry, verifies the checksum, fires callbacks, and emits an ACK.
func (r *Receiver) finalize(id string, sess *session, wantChecksum string) {
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()

	got := hex.EncodeToString(sess.hash.Sum(nil))
	if got != wantChecksum {
		fmt.Printf("[error] checksum mismatch for %s (want %s got %s)\n",
			sess.meta.Name, wantChecksum, got)
		os.Remove(sess.outPath)
		r.sendAck(sess.peerAddr, id, false, fmt.Sprintf("checksum mismatch (want %s got %s)", wantChecksum, got))
		return
	}
	if sess.folderName != "" {
		fmt.Printf("[folder] file saved: %s\n", sess.outPath)
		if sess.onFolderDone != nil {
			sess.onFolderDone(sess.folderName, sess.folderRel, sess.outPath)
		}
	} else {
		fmt.Printf("[recv] file saved: %s\n", sess.outPath)
		if sess.onComplete != nil {
			sess.onComplete(sess.outPath)
		}
	}
	r.sendAck(sess.peerAddr, id, true, "")
}

// abortSession cleans up a partially-received file and sends a failure ACK.
// Caller must hold sess.mu.
func (r *Receiver) abortSession(id string, sess *session, reason string) {
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()
	if sess.file != nil {
		sess.file.Close()
		sess.file = nil
	}
	os.Remove(sess.outPath)
	r.sendAck(sess.peerAddr, id, false, reason)
}

func (r *Receiver) sendAck(peerAddr, id string, ok bool, errStr string) {
	if r.ackSend == nil || peerAddr == "" {
		return
	}
	msg, err := protocol.NewMessage(protocol.MsgFileAck, protocol.FileAckPayload{
		ID: id, OK: ok, Err: errStr,
	})
	if err != nil {
		return
	}
	r.ackSend(peerAddr, msg)
}

// AbortPeer cleans up any in-flight receives for the given peer (e.g. when
// the peer disconnects). Removes partial files from disk.
func (r *Receiver) AbortPeer(peerAddr string) {
	r.mu.Lock()
	ids := make([]string, 0)
	for id, sess := range r.sessions {
		if sess.peerAddr == peerAddr {
			ids = append(ids, id)
		}
	}
	r.mu.Unlock()
	for _, id := range ids {
		r.mu.Lock()
		sess, ok := r.sessions[id]
		if ok {
			delete(r.sessions, id)
		}
		r.mu.Unlock()
		if !ok {
			continue
		}
		sess.mu.Lock()
		if sess.file != nil {
			sess.file.Close()
		}
		os.Remove(sess.outPath)
		sess.mu.Unlock()
		fmt.Printf("[recv] aborted incomplete transfer %s (peer %s disconnected)\n", sess.meta.Name, peerAddr)
	}
}

// --- helpers ---

// generateID returns a cryptographically random hex string as a transfer ID.
func generateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// rand.Read should never fail on a healthy OS.
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return hex.EncodeToString(b)
}

// --- Shared folder transfers ---

// HandleFolderMeta processes a folder_file_meta payload. It creates a receive
// session that streams chunks directly into folderDir/relPath. peerAddr is
// the sender's address; it is forwarded to the receive-side progress callback.
// onComplete is called with (folderName, relPath, absPath) once verified.
func (r *Receiver) HandleFolderMeta(raw json.RawMessage, folderDir, peerAddr string, onComplete func(folderName, relPath, absPath string)) {
	var meta protocol.FolderFileMetaPayload
	if err := json.Unmarshal(raw, &meta); err != nil {
		fmt.Printf("[warn] bad folder_file_meta: %v\n", err)
		return
	}

	// Sanitise the relative path to prevent directory traversal.
	relPath := filepath.FromSlash(filepath.Clean(meta.RelPath))
	if filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "..") {
		fmt.Printf("[warn] unsafe folder rel_path rejected: %s\n", meta.RelPath)
		return
	}

	outPath := filepath.Join(folderDir, relPath)

	// Last-write-wins: skip if our local copy is the same age or newer.
	if info, statErr := os.Stat(outPath); statErr == nil {
		if info.ModTime().Unix() >= meta.ModTime {
			return
		}
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		fmt.Printf("[error] create folder dir: %v\n", err)
		return
	}
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Printf("[error] open folder file %s: %v\n", outPath, err)
		return
	}

	r.mu.Lock()
	r.sessions[meta.ID] = &session{
		meta: protocol.FileMetaPayload{
			ID:       meta.ID,
			Name:     meta.RelPath,
			Size:     meta.Size,
			Checksum: meta.Checksum,
		},
		file:         f,
		hash:         sha256.New(),
		outPath:      outPath,
		peerAddr:     peerAddr,
		folderName:   meta.Folder,
		folderRel:    filepath.ToSlash(relPath),
		onFolderDone: onComplete,
		progressFn:   r.progressFn,
	}
	r.mu.Unlock()

	fmt.Printf("[folder] incoming %s/%s (%d bytes)\n", meta.Folder, meta.RelPath, meta.Size)
}

// SendFolderFile streams absPath as a shared-folder file transfer.  Reads the
// file exactly once; SHA-256 is computed streaming and sent as a trailer.
func SendFolderFile(folderName, relPath, absPath string, modTime int64, peerAddr string, sendFn SendFunc, progressFn ProgressFunc) error {
	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	id := generateID()
	metaMsg, err := protocol.NewMessage(protocol.MsgFolderFileMeta, protocol.FolderFileMetaPayload{
		Folder:  folderName,
		ID:      id,
		RelPath: relPath,
		Size:    info.Size(),
		ModTime: modTime,
		// Checksum follows as MsgFileChecksum trailer.
	})
	if err != nil {
		return fmt.Errorf("build folder_file_meta: %w", err)
	}
	sendFn(metaMsg)

	checksum, err := streamChunks(f, id, info.Size(), relPath, peerAddr, sendFn, progressFn)
	if err != nil {
		return err
	}

	trailer, err := protocol.NewMessage(protocol.MsgFileChecksum, protocol.FileChecksumPayload{
		ID:       id,
		Checksum: checksum,
	})
	if err != nil {
		return fmt.Errorf("build file_checksum: %w", err)
	}
	sendFn(trailer)
	return nil
}
