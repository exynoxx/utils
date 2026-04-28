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

const chunkSize = 64 * 1024 // 64 KB per chunk

// SendFunc is the signature for enqueuing a message to a specific peer.
type SendFunc func(msg protocol.Message)

// ProgressFunc is called periodically during transfers with the file name and
// current percentage (0–100).  It may be nil.
type ProgressFunc func(name string, pct float64)

// Send reads the file at path, announces it via file_meta, then streams
// file_chunk messages using sendFn.  progressFn (may be nil) is called with
// the current percentage after each chunk.
func Send(path string, sendFn SendFunc, progressFn ProgressFunc) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	checksum, err := sha256File(f)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek: %w", err)
	}

	id := generateID()
	metaMsg, err := protocol.NewMessage(protocol.MsgFileMeta, protocol.FileMetaPayload{
		ID:       id,
		Name:     filepath.Base(path),
		Size:     info.Size(),
		Checksum: checksum,
	})
	if err != nil {
		return fmt.Errorf("build file_meta: %w", err)
	}
	sendFn(metaMsg)

	buf := make([]byte, chunkSize)
	index := 0
	var sent int64
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			sent += int64(n)
			final := readErr == io.EOF || sent >= info.Size()
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			chunkMsg, err := protocol.NewMessage(protocol.MsgFileChunk, protocol.FileChunkPayload{
				ID:    id,
				Index: index,
				Data:  chunk,
				Final: final,
			})
			if err != nil {
				return fmt.Errorf("build file_chunk: %w", err)
			}
			sendFn(chunkMsg)

			var pct float64
			if info.Size() > 0 {
				pct = float64(sent) / float64(info.Size()) * 100
			}
			fmt.Printf("\r[send] %s  %.1f%%", filepath.Base(path), pct)
			if progressFn != nil {
				progressFn(filepath.Base(path), pct)
			}
			index++
			if final {
				fmt.Println()
				break
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("read file: %w", readErr)
		}
	}
	return nil
}

// --- Receiver ---

// session tracks an in-progress file receive.
// Chunks are streamed directly to disk as they arrive (TCP guarantees order),
// so memory usage is bounded to one chunk at a time regardless of file size.
type session struct {
	meta       protocol.FileMetaPayload
	file       *os.File   // open for writing; closed when Final chunk processed
	hash       hash.Hash  // running sha256 updated per chunk
	received   int64
	outPath    string // final destination path
	mu         sync.Mutex
	onComplete func(string)
	progressFn ProgressFunc // may be nil
}

// Receiver collects incoming file_meta and file_chunk messages and writes
// completed transfers to outDir.
type Receiver struct {
	outDir     string
	mu         sync.Mutex
	sessions   map[string]*session
	progressFn ProgressFunc // applied to all new receive sessions; may be nil
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

// HandleMeta processes a file_meta payload (raw JSON).
// onComplete is called with the final file path once all chunks have arrived.
func (r *Receiver) HandleMeta(raw json.RawMessage, onComplete func(path string)) {
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
		onComplete: onComplete,
		progressFn: r.progressFn,
	}
	r.mu.Unlock()

	fmt.Printf("[recv] incoming file: %s (%d bytes)\n", meta.Name, meta.Size)
}

// HandleChunk processes a file_chunk payload (raw JSON).
// Each chunk is written directly to disk; no full-file buffering occurs.
func (r *Receiver) HandleChunk(raw json.RawMessage) {
	var chunk protocol.FileChunkPayload
	if err := json.Unmarshal(raw, &chunk); err != nil {
		fmt.Printf("[warn] bad file_chunk: %v\n", err)
		return
	}

	r.mu.Lock()
	sess, ok := r.sessions[chunk.ID]
	if ok && chunk.Final {
		delete(r.sessions, chunk.ID)
	}
	r.mu.Unlock()

	if !ok {
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if _, err := sess.file.Write(chunk.Data); err != nil {
		fmt.Printf("[error] write chunk for %s: %v\n", sess.meta.Name, err)
		sess.file.Close()
		os.Remove(sess.outPath)
		return
	}
	sess.hash.Write(chunk.Data)
	sess.received += int64(len(chunk.Data))

	var pct float64
	if sess.meta.Size > 0 {
		pct = float64(sess.received) / float64(sess.meta.Size) * 100
	}
	fmt.Printf("\r[recv] %s  %.1f%%", sess.meta.Name, pct)
	if sess.progressFn != nil {
		sess.progressFn(sess.meta.Name, pct)
	}

	if chunk.Final {
		fmt.Println()
		if err := sess.file.Close(); err != nil {
			fmt.Printf("[error] close output file %s: %v\n", sess.outPath, err)
			os.Remove(sess.outPath)
			return
		}
		got := hex.EncodeToString(sess.hash.Sum(nil))
		if got != sess.meta.Checksum {
			fmt.Printf("[error] checksum mismatch for %s (want %s got %s)\n",
				sess.meta.Name, sess.meta.Checksum, got)
			os.Remove(sess.outPath)
			return
		}
		fmt.Printf("[recv] file saved: %s\n", sess.outPath)
		if sess.onComplete != nil {
			sess.onComplete(sess.outPath)
		}
	}
}

// --- helpers ---

func sha256File(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

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
// session that streams chunks directly into folderDir/relPath.
// onComplete is called with (folderName, relPath, absPath) once verified.
func (r *Receiver) HandleFolderMeta(raw json.RawMessage, folderDir string, onComplete func(folderName, relPath, absPath string)) {
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
		file:    f,
		hash:    sha256.New(),
		outPath: outPath,
		onComplete: func(path string) {
			if onComplete != nil {
				onComplete(meta.Folder, filepath.ToSlash(relPath), path)
			}
		},
	}
	r.mu.Unlock()

	fmt.Printf("[folder] incoming %s/%s (%d bytes)\n", meta.Folder, meta.RelPath, meta.Size)
}

// SendFolderFile reads absPath and sends it as a shared-folder file transfer.
// relPath is forward-slash relative to the folder root. Chunks are sent as
// ordinary MsgFileChunk messages (same ID-keyed routing as normal transfers).
func SendFolderFile(folderName, relPath, absPath string, modTime int64, sendFn SendFunc) error {
	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	checksum, err := sha256File(f)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek: %w", err)
	}

	id := generateID()
	metaMsg, err := protocol.NewMessage(protocol.MsgFolderFileMeta, protocol.FolderFileMetaPayload{
		Folder:   folderName,
		ID:       id,
		RelPath:  relPath,
		Size:     info.Size(),
		Checksum: checksum,
		ModTime:  modTime,
	})
	if err != nil {
		return fmt.Errorf("build folder_file_meta: %w", err)
	}
	sendFn(metaMsg)

	buf := make([]byte, chunkSize)
	index := 0
	var sent int64
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			sent += int64(n)
			final := readErr == io.EOF || sent >= info.Size()
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			chunkMsg, err := protocol.NewMessage(protocol.MsgFileChunk, protocol.FileChunkPayload{
				ID:    id,
				Index: index,
				Data:  chunk,
				Final: final,
			})
			if err != nil {
				return fmt.Errorf("build file_chunk: %w", err)
			}
			sendFn(chunkMsg)
			index++
			if final {
				break
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("read file: %w", readErr)
		}
	}
	return nil
}
