package protocol

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"testing"
)

// TestWireRoundTripWithBin verifies that a message carrying a binary trailer
// survives WriteMessage → ReadMessage unchanged. This is the hot path for
// file chunks.
func TestWireRoundTripWithBin(t *testing.T) {
	bin := make([]byte, 1024*1024) // 1 MB of random bytes
	if _, err := rand.Read(bin); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	payload, _ := json.Marshal(FileChunkPayload{ID: "abc", Index: 7, Final: false})
	msg := Message{Type: MsgFileChunk, Payload: payload, Bin: bin}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Type != msg.Type {
		t.Errorf("type: got %q want %q", got.Type, msg.Type)
	}
	if !bytes.Equal(got.Payload, msg.Payload) {
		t.Errorf("payload mismatch")
	}
	if !bytes.Equal(got.Bin, msg.Bin) {
		t.Errorf("bin mismatch (len got=%d want=%d)", len(got.Bin), len(msg.Bin))
	}
}

// TestWireRoundTripNoBin verifies that a plain JSON-only message (e.g. chat,
// meta, ack) survives the new wire format too.
func TestWireRoundTripNoBin(t *testing.T) {
	payload, _ := json.Marshal(ChatPayload{Nick: "alice", Text: "hello", Time: 12345})
	msg := Message{Type: MsgChat, Payload: payload}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Type != msg.Type {
		t.Errorf("type mismatch")
	}
	if len(got.Bin) != 0 {
		t.Errorf("unexpected Bin on JSON-only message: %d bytes", len(got.Bin))
	}
	var p ChatPayload
	if err := json.Unmarshal(got.Payload, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Nick != "alice" || p.Text != "hello" {
		t.Errorf("chat payload corrupted: %+v", p)
	}
}

// TestMultipleMessagesInStream verifies the framing: two messages back-to-back
// in one buffer should decode independently.
func TestMultipleMessagesInStream(t *testing.T) {
	var buf bytes.Buffer

	bin1 := []byte{1, 2, 3, 4, 5}
	msg1 := Message{Type: MsgFileChunk, Payload: json.RawMessage(`{"id":"x"}`), Bin: bin1}
	msg2 := Message{Type: MsgChat, Payload: json.RawMessage(`{"text":"hi"}`)}

	if err := WriteMessage(&buf, msg1); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if err := WriteMessage(&buf, msg2); err != nil {
		t.Fatalf("write 2: %v", err)
	}

	got1, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read 1: %v", err)
	}
	if !bytes.Equal(got1.Bin, bin1) {
		t.Errorf("msg1 bin mismatch")
	}

	got2, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if got2.Type != MsgChat || len(got2.Bin) != 0 {
		t.Errorf("msg2 wrong shape: %+v", got2)
	}
}

// TestCryptoRoundTripWithBin checks that the encrypted wire format also
// carries the binary trailer correctly.
func TestCryptoRoundTripWithBin(t *testing.T) {
	kpA, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("keypair A: %v", err)
	}
	kpB, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("keypair B: %v", err)
	}

	var buf bytes.Buffer
	ccSend := NewCryptoConn(&buf, kpA.Private, kpB.Public)
	ccRecv := NewCryptoConn(&buf, kpB.Private, kpA.Public)

	bin := make([]byte, 256*1024)
	for i := range bin {
		bin[i] = byte(i)
	}
	msg := Message{
		Type:    MsgFileChunk,
		Payload: json.RawMessage(`{"id":"y","index":0,"final":true}`),
		Bin:     bin,
	}
	if err := ccSend.WriteMessage(msg); err != nil {
		t.Fatalf("crypto write: %v", err)
	}
	got, err := ccRecv.ReadMessage()
	if err != nil {
		t.Fatalf("crypto read: %v", err)
	}
	if got.Type != msg.Type {
		t.Errorf("type mismatch")
	}
	if !bytes.Equal(got.Bin, bin) {
		t.Errorf("bin mismatch")
	}
}
