package protocol

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"golang.org/x/crypto/nacl/box"
)

// KeyPair holds a Curve25519 key pair for NaCl box encryption.
type KeyPair struct {
	Public  [32]byte
	Private [32]byte
}

// GenerateKeyPair generates a new Curve25519 key pair.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key pair: %w", err)
	}
	return &KeyPair{Public: *pub, Private: *priv}, nil
}

// CryptoConn wraps an io.ReadWriter and transparently encrypts/decrypts
// messages using NaCl box (Curve25519 + XSalsa20-Poly1305).
// Call NewCryptoConn after the TCP connection is established and public keys
// have been exchanged via the plain-text CryptoHandshake message.
type CryptoConn struct {
	rw        io.ReadWriter
	sharedKey [32]byte
}

// NewCryptoConn pre-computes the shared key from the local private key and
// the remote peer's public key, then returns a CryptoConn ready for use.
func NewCryptoConn(rw io.ReadWriter, localPriv, remotePub [32]byte) *CryptoConn {
	cc := &CryptoConn{rw: rw}
	box.Precompute(&cc.sharedKey, &remotePub, &localPriv)
	return cc
}

// WriteMessage encrypts msg and writes it with the wire format:
//
//	[4-byte big-endian length of (nonce+ciphertext)][24-byte nonce][ciphertext]
//
// The plaintext is itself [4-byte json_len][JSON][bin], matching the unencrypted
// wire format minus the outer 8-byte length pair (sealed_len bounds the whole).
func (c *CryptoConn) WriteMessage(msg Message) error {
	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Build plaintext: [4-byte json_len][JSON][bin]
	plain := make([]byte, 4+len(jsonBytes)+len(msg.Bin))
	binary.BigEndian.PutUint32(plain[0:4], uint32(len(jsonBytes)))
	copy(plain[4:4+len(jsonBytes)], jsonBytes)
	copy(plain[4+len(jsonBytes):], msg.Bin)

	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	// SealAfterPrecomputation appends ciphertext to the out slice.
	// Passing nonce[:] as out means sealed = [nonce | ciphertext].
	sealed := box.SealAfterPrecomputation(nonce[:], plain, &nonce, &c.sharedKey)

	if err := binary.Write(c.rw, binary.BigEndian, uint32(len(sealed))); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if _, err := c.rw.Write(sealed); err != nil {
		return fmt.Errorf("write sealed: %w", err)
	}
	return nil
}

// ReadMessage reads and authenticates a message written by WriteMessage.
func (c *CryptoConn) ReadMessage() (Message, error) {
	var length uint32
	if err := binary.Read(c.rw, binary.BigEndian, &length); err != nil {
		return Message{}, err
	}
	if length > maxMessageSize {
		return Message{}, fmt.Errorf("encrypted message too large: %d bytes", length)
	}

	sealed := make([]byte, length)
	if _, err := io.ReadFull(c.rw, sealed); err != nil {
		return Message{}, fmt.Errorf("read sealed: %w", err)
	}

	if len(sealed) < 24 {
		return Message{}, fmt.Errorf("sealed data too short for nonce")
	}
	var nonce [24]byte
	copy(nonce[:], sealed[:24])

	plain, ok := box.OpenAfterPrecomputation(nil, sealed[24:], &nonce, &c.sharedKey)
	if !ok {
		return Message{}, fmt.Errorf("decryption failed: authentication tag mismatch")
	}

	// Plaintext is [4-byte json_len][JSON][bin].
	if len(plain) < 4 {
		return Message{}, fmt.Errorf("decrypted plaintext too short")
	}
	jsonLen := binary.BigEndian.Uint32(plain[0:4])
	if int(jsonLen) > len(plain)-4 {
		return Message{}, fmt.Errorf("decrypted json_len overruns plaintext")
	}
	var msg Message
	if err := json.Unmarshal(plain[4:4+jsonLen], &msg); err != nil {
		return Message{}, fmt.Errorf("unmarshal: %w", err)
	}
	if int(jsonLen) < len(plain)-4 {
		msg.Bin = make([]byte, len(plain)-4-int(jsonLen))
		copy(msg.Bin, plain[4+jsonLen:])
	}
	return msg, nil
}
