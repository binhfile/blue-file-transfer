// Package crypto provides encrypted stream wrappers for Bluetooth connections.
//
// Key exchange protocol:
//  1. Both sides derive a shared key from the user's password using HKDF-SHA256
//  2. Server sends a random 32-byte nonce to the client
//  3. Client responds with its own random 32-byte nonce
//  4. Both sides derive the final session key: HKDF(password, server_nonce || client_nonce)
//  5. All subsequent data is encrypted with AES-256-GCM per-message
//
// Each encrypted message: [4-byte length][12-byte nonce][ciphertext+16-byte tag]
// The nonce is a 64-bit counter (unique per direction) + 4 random bytes.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/hkdf"
)

const (
	// NonceSize is the GCM nonce size.
	NonceSize = 12
	// TagSize is the GCM authentication tag size.
	TagSize = 16
	// MaxFrameSize is the maximum encrypted frame payload.
	MaxFrameSize = 128 * 1024
	// HandshakeNonceSize is the size of the handshake nonce exchange.
	HandshakeNonceSize = 32
)

// DeriveKey derives a 256-bit encryption key from password and nonces using HKDF-SHA256.
func DeriveKey(password string, serverNonce, clientNonce []byte) ([]byte, error) {
	salt := make([]byte, len(serverNonce)+len(clientNonce))
	copy(salt, serverNonce)
	copy(salt[len(serverNonce):], clientNonce)

	hkdfReader := hkdf.New(sha256.New, []byte(password), salt, []byte("bft-session-key-v1"))
	key := make([]byte, 32) // AES-256
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	return key, nil
}

// EncryptedStream wraps a ReadWriter with AES-256-GCM encryption.
type EncryptedStream struct {
	conn     io.ReadWriter
	gcm      cipher.AEAD
	writeMu  sync.Mutex
	writeSeq atomic.Uint64
	readSeq  atomic.Uint64
	readBuf  []byte // buffered decrypted data from partial reads
}

// NewEncryptedStream creates a new encrypted stream from a key.
func NewEncryptedStream(conn io.ReadWriter, key []byte) (*EncryptedStream, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &EncryptedStream{
		conn: conn,
		gcm:  gcm,
	}, nil
}

// Write encrypts data and writes it as a framed message.
// Frame format: [4-byte payload length][12-byte nonce][ciphertext (len + 16 tag)]
func (s *EncryptedStream) Write(plaintext []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Build unique nonce from counter
	nonce := make([]byte, NonceSize)
	binary.LittleEndian.PutUint64(nonce, s.writeSeq.Add(1))
	// Fill remaining 4 bytes with random for additional uniqueness
	rand.Read(nonce[8:])

	ciphertext := s.gcm.Seal(nil, nonce, plaintext, nil)

	// Write frame: length(4) + nonce(12) + ciphertext
	frameLen := uint32(NonceSize + len(ciphertext))
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, frameLen)

	// Combine into single write
	frame := make([]byte, 4+int(frameLen))
	copy(frame, header)
	copy(frame[4:], nonce)
	copy(frame[4+NonceSize:], ciphertext)

	if _, err := s.conn.Write(frame); err != nil {
		return 0, fmt.Errorf("write encrypted frame: %w", err)
	}
	return len(plaintext), nil
}

// Read decrypts the next frame and returns plaintext.
func (s *EncryptedStream) Read(p []byte) (int, error) {
	// Return buffered data first
	if len(s.readBuf) > 0 {
		n := copy(p, s.readBuf)
		s.readBuf = s.readBuf[n:]
		return n, nil
	}

	// Read frame header (4 bytes length)
	header := make([]byte, 4)
	if _, err := io.ReadFull(s.conn, header); err != nil {
		return 0, err
	}

	frameLen := binary.LittleEndian.Uint32(header)
	if frameLen > MaxFrameSize {
		return 0, fmt.Errorf("encrypted frame too large: %d", frameLen)
	}
	if frameLen < NonceSize+TagSize {
		return 0, fmt.Errorf("encrypted frame too small: %d", frameLen)
	}

	// Read nonce + ciphertext
	frame := make([]byte, frameLen)
	if _, err := io.ReadFull(s.conn, frame); err != nil {
		return 0, fmt.Errorf("read encrypted frame: %w", err)
	}

	nonce := frame[:NonceSize]
	ciphertext := frame[NonceSize:]

	plaintext, err := s.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return 0, fmt.Errorf("decrypt: %w", err)
	}

	n := copy(p, plaintext)
	if n < len(plaintext) {
		s.readBuf = plaintext[n:]
	}
	return n, nil
}

// ServerHandshake performs the server side of the key exchange.
// Returns the encrypted stream wrapping conn.
func ServerHandshake(conn io.ReadWriter, password string) (*EncryptedStream, error) {
	// Generate and send server nonce
	serverNonce := make([]byte, HandshakeNonceSize)
	if _, err := rand.Read(serverNonce); err != nil {
		return nil, fmt.Errorf("generate server nonce: %w", err)
	}
	if _, err := conn.Write(serverNonce); err != nil {
		return nil, fmt.Errorf("send server nonce: %w", err)
	}

	// Read client nonce
	clientNonce := make([]byte, HandshakeNonceSize)
	if _, err := io.ReadFull(conn, clientNonce); err != nil {
		return nil, fmt.Errorf("read client nonce: %w", err)
	}

	key, err := DeriveKey(password, serverNonce, clientNonce)
	if err != nil {
		return nil, err
	}

	return NewEncryptedStream(conn, key)
}

// ClientHandshake performs the client side of the key exchange.
// Returns the encrypted stream wrapping conn.
func ClientHandshake(conn io.ReadWriter, password string) (*EncryptedStream, error) {
	// Read server nonce
	serverNonce := make([]byte, HandshakeNonceSize)
	if _, err := io.ReadFull(conn, serverNonce); err != nil {
		return nil, fmt.Errorf("read server nonce: %w", err)
	}

	// Generate and send client nonce
	clientNonce := make([]byte, HandshakeNonceSize)
	if _, err := rand.Read(clientNonce); err != nil {
		return nil, fmt.Errorf("generate client nonce: %w", err)
	}
	if _, err := conn.Write(clientNonce); err != nil {
		return nil, fmt.Errorf("send client nonce: %w", err)
	}

	key, err := DeriveKey(password, serverNonce, clientNonce)
	if err != nil {
		return nil, err
	}

	return NewEncryptedStream(conn, key)
}
