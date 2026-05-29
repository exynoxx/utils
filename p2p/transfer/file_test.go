package transfer

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"p2p/protocol"
)

// wireRoundTrip pushes msg through WriteMessage→ReadMessage so the test
// exercises the real binary-trailer wire format end-to-end.
func wireRoundTrip(t *testing.T, msg protocol.Message) protocol.Message {
	t.Helper()
	var buf bytes.Buffer
	if err := protocol.WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := protocol.ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	return got
}

// e2eHarness wires Send→[wire encode/decode]→Receiver dispatch synchronously.
// Each msg goes through the actual protocol.WriteMessage/ReadMessage path.
type e2eHarness struct {
	t            *testing.T
	recv         *Receiver
	complete     chan string
	folderDone   chan string
	ack          chan protocol.FileAckPayload
	corruptChunk int  // 1-based chunk index to corrupt (0 = none)
	chunkCount   int
}

func newHarness(t *testing.T, outDir string) *e2eHarness {
	h := &e2eHarness{
		t:          t,
		recv:       NewReceiver(outDir),
		complete:   make(chan string, 8),
		folderDone: make(chan string, 32),
		ack:        make(chan protocol.FileAckPayload, 8),
	}
	h.recv.SetAckSender(func(peerAddr string, msg protocol.Message) {
		if msg.Type != protocol.MsgFileAck {
			return
		}
		// Round-trip ACK through the wire too.
		ack := wireRoundTrip(t, msg)
		var p protocol.FileAckPayload
		if err := json.Unmarshal(ack.Payload, &p); err != nil {
			t.Errorf("bad ack payload: %v", err)
			return
		}
		h.ack <- p
	})
	return h
}

func (h *e2eHarness) sendFn(folderDir string) SendFunc {
	return func(msg protocol.Message) {
		// Optionally corrupt a specific chunk to test failure path.
		if h.corruptChunk > 0 && msg.Type == protocol.MsgFileChunk {
			h.chunkCount++
			if h.chunkCount == h.corruptChunk && len(msg.Bin) > 0 {
				msg.Bin[0] ^= 0xFF
			}
		}
		decoded := wireRoundTrip(h.t, msg)
		switch decoded.Type {
		case protocol.MsgFileMeta:
			h.recv.HandleMeta(decoded.Payload, "peerX", func(path string) {
				h.complete <- path
			})
		case protocol.MsgFolderFileMeta:
			h.recv.HandleFolderMeta(decoded.Payload, folderDir, "peerX", func(_, _, abs string) {
				h.folderDone <- abs
			})
		case protocol.MsgFileChunk:
			h.recv.HandleChunk(decoded.Payload, decoded.Bin)
		case protocol.MsgFileChecksum:
			h.recv.HandleChecksum(decoded.Payload)
		}
	}
}

// TestSendReceiveFile_E2E sends a multi-chunk file through the full stack
// (meta → chunks via binary wire format → checksum trailer → ACK).
func TestSendReceiveFile_E2E(t *testing.T) {
	tmp := t.TempDir()

	// 5 MB + odd bytes so we exercise a partial final chunk.
	const size = 5*1024*1024 + 777
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	src := filepath.Join(tmp, "movie.bin")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}

	outDir := filepath.Join(tmp, "downloads")
	h := newHarness(t, outDir)

	if err := Send(src, "peerX", h.sendFn(""), nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Completion callback should have fired.
	var got string
	select {
	case got = <-h.complete:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for file completion")
	}

	// Receiver writes to outDir/<basename>.
	want := filepath.Join(outDir, "movie.bin")
	if got != want {
		t.Errorf("output path: got %q want %q", got, want)
	}

	// Bytes must match exactly.
	gotBytes, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(gotBytes, data) {
		t.Fatalf("file mismatch: got %d bytes, want %d (equal=%v)",
			len(gotBytes), len(data), bytes.Equal(gotBytes, data))
	}

	// Positive ACK.
	select {
	case ack := <-h.ack:
		if !ack.OK {
			t.Fatalf("expected OK ACK, got fail: %s", ack.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ACK")
	}
}

// TestSendReceiveFolder_E2E sends three files of varying sizes as a folder and
// verifies each arrives intact under the correct relative path.
func TestSendReceiveFolder_E2E(t *testing.T) {
	tmp := t.TempDir()
	stage := filepath.Join(tmp, "stage")
	if err := os.MkdirAll(stage, 0o750); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}

	// Three files: small, exactly one chunk, two-and-a-bit chunks.
	files := map[string]int{
		"small.txt":            10,
		"medium/exact.bin":     chunkSize,
		"deeper/big-movie.bin": 2*chunkSize + 333,
	}
	wantData := make(map[string][]byte, len(files))
	for rel, sz := range files {
		buf := make([]byte, sz)
		if _, err := rand.Read(buf); err != nil {
			t.Fatalf("rand: %v", err)
		}
		wantData[rel] = buf
		abs := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatalf("mkdir parent: %v", err)
		}
		if err := os.WriteFile(abs, buf, 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	folderDir := filepath.Join(tmp, "out", "myfolder")
	h := newHarness(t, "") // outDir unused for folder transfers

	for rel := range files {
		abs := filepath.Join(stage, filepath.FromSlash(rel))
		if err := SendFolderFile("myfolder", rel, abs, time.Now().Unix(), "peerX", h.sendFn(folderDir), nil); err != nil {
			t.Fatalf("SendFolderFile %s: %v", rel, err)
		}
	}

	// Expect 3 completions + 3 ACKs.
	timeout := time.After(10 * time.Second)
	doneCount, ackCount := 0, 0
	for doneCount < len(files) || ackCount < len(files) {
		select {
		case <-h.folderDone:
			doneCount++
		case ack := <-h.ack:
			if !ack.OK {
				t.Fatalf("folder ACK failed: %s", ack.Err)
			}
			ackCount++
		case <-timeout:
			t.Fatalf("timeout: done=%d ack=%d of %d", doneCount, ackCount, len(files))
		}
	}

	// Verify bytes for each file at its expected path.
	for rel, want := range wantData {
		abs := filepath.Join(folderDir, filepath.FromSlash(rel))
		got, err := os.ReadFile(abs)
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("content mismatch for %s (got %d want %d)", rel, len(got), len(want))
		}
	}
}

// TestCorruptChunk_NACK verifies the integrity guarantee: a flipped bit in any
// chunk must result in a checksum mismatch and a NACK back to the sender, and
// the partial file must be removed.
func TestCorruptChunk_NACK(t *testing.T) {
	tmp := t.TempDir()
	const size = 3*chunkSize + 50 // multiple chunks so we can flip a middle one
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand: %v", err)
	}
	src := filepath.Join(tmp, "src.bin")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	outDir := filepath.Join(tmp, "downloads")
	h := newHarness(t, outDir)
	h.corruptChunk = 2 // flip a bit in the second chunk

	if err := Send(src, "peerX", h.sendFn(""), nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Must get a failed ACK.
	select {
	case ack := <-h.ack:
		if ack.OK {
			t.Fatalf("expected NACK from corrupted send, got OK")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for NACK")
	}

	// Corrupted file must not be left on disk.
	if _, err := os.Stat(filepath.Join(outDir, "src.bin")); !os.IsNotExist(err) {
		t.Errorf("expected corrupted file to be removed, stat err=%v", err)
	}

	// onComplete must NOT fire on a failed transfer.
	select {
	case path := <-h.complete:
		t.Errorf("onComplete fired despite corruption: %s", path)
	default:
	}
}

// TestEmptyFile_E2E sends a zero-byte file. The sender must emit a synthetic
// empty final chunk so the receiver closes the file handle and finalises.
func TestEmptyFile_E2E(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "empty.bin")
	if err := os.WriteFile(src, nil, 0o600); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	outDir := filepath.Join(tmp, "downloads")
	h := newHarness(t, outDir)

	if err := Send(src, "peerX", h.sendFn(""), nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case path := <-h.complete:
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat completed file: %v", err)
		}
		if info.Size() != 0 {
			t.Errorf("empty file got size %d, want 0", info.Size())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for empty-file completion")
	}

	select {
	case ack := <-h.ack:
		if !ack.OK {
			t.Errorf("empty-file ACK failed: %s", ack.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for empty-file ACK")
	}
}
