package crypto

import (
	"bytes"
	"io"
	"testing"
)

type pipeRW struct {
	r io.Reader
	w io.Writer
}

func (p *pipeRW) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeRW) Write(b []byte) (int, error) { return p.w.Write(b) }

func newPipePair() (io.ReadWriter, io.ReadWriter) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	return &pipeRW{r: r1, w: w2}, &pipeRW{r: r2, w: w1}
}

func TestDeriveKey(t *testing.T) {
	key1, err := DeriveKey("password", []byte("server"), []byte("client"))
	if err != nil {
		t.Fatal(err)
	}
	if len(key1) != 32 {
		t.Errorf("key length = %d, want 32", len(key1))
	}

	// Same inputs = same key
	key2, _ := DeriveKey("password", []byte("server"), []byte("client"))
	if !bytes.Equal(key1, key2) {
		t.Error("same inputs should produce same key")
	}

	// Different password = different key
	key3, _ := DeriveKey("other", []byte("server"), []byte("client"))
	if bytes.Equal(key1, key3) {
		t.Error("different password should produce different key")
	}

	// Different nonce = different key
	key4, _ := DeriveKey("password", []byte("other"), []byte("client"))
	if bytes.Equal(key1, key4) {
		t.Error("different nonce should produce different key")
	}
}

func TestEncryptedStream_RoundTrip(t *testing.T) {
	key, _ := DeriveKey("test", []byte("a"), []byte("b"))

	side1, side2 := newPipePair()

	enc1, err := NewEncryptedStream(side1, key)
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := NewEncryptedStream(side2, key)
	if err != nil {
		t.Fatal(err)
	}

	// Write from enc1, read from enc2
	msg := []byte("hello encrypted world")
	go func() {
		enc1.Write(msg)
	}()

	buf := make([]byte, 1024)
	n, err := enc2.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], msg) {
		t.Errorf("got %q, want %q", buf[:n], msg)
	}
}

func TestEncryptedStream_LargeData(t *testing.T) {
	key, _ := DeriveKey("test", []byte("a"), []byte("b"))

	side1, side2 := newPipePair()
	enc1, _ := NewEncryptedStream(side1, key)
	enc2, _ := NewEncryptedStream(side2, key)

	// 64KB of data
	data := make([]byte, 64*1024)
	for i := range data {
		data[i] = byte(i)
	}

	go func() {
		enc1.Write(data)
	}()

	var received []byte
	buf := make([]byte, 4096)
	for len(received) < len(data) {
		n, err := enc2.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		received = append(received, buf[:n]...)
	}

	if !bytes.Equal(received, data) {
		t.Errorf("data mismatch: got %d bytes, want %d", len(received), len(data))
	}
}

func TestEncryptedStream_MultipleMessages(t *testing.T) {
	key, _ := DeriveKey("test", []byte("a"), []byte("b"))

	side1, side2 := newPipePair()
	enc1, _ := NewEncryptedStream(side1, key)
	enc2, _ := NewEncryptedStream(side2, key)

	messages := []string{"first", "second message", "third one here"}

	go func() {
		for _, m := range messages {
			enc1.Write([]byte(m))
		}
	}()

	for _, expected := range messages {
		buf := make([]byte, 1024)
		n, err := enc2.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:n]) != expected {
			t.Errorf("got %q, want %q", buf[:n], expected)
		}
	}
}

func TestEncryptedStream_WrongKey(t *testing.T) {
	key1, _ := DeriveKey("test1", []byte("a"), []byte("b"))
	key2, _ := DeriveKey("test2", []byte("a"), []byte("b"))

	side1, side2 := newPipePair()
	enc1, _ := NewEncryptedStream(side1, key1)
	enc2, _ := NewEncryptedStream(side2, key2) // different key!

	go func() {
		enc1.Write([]byte("secret"))
	}()

	buf := make([]byte, 1024)
	_, err := enc2.Read(buf)
	if err == nil {
		t.Fatal("expected decryption error with wrong key")
	}
}

func TestHandshake(t *testing.T) {
	side1, side2 := newPipePair()
	password := "shared-secret"

	var serverStream *EncryptedStream
	var serverErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		serverStream, serverErr = ServerHandshake(side2, password)
	}()

	clientStream, err := ClientHandshake(side1, password)
	if err != nil {
		t.Fatal(err)
	}

	<-done
	if serverErr != nil {
		t.Fatal(serverErr)
	}

	// Server -> Client
	go func() {
		serverStream.Write([]byte("hello from server"))
	}()

	buf := make([]byte, 1024)
	n, err := clientStream.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello from server" {
		t.Errorf("got %q", buf[:n])
	}

	// Client -> Server
	go func() {
		clientStream.Write([]byte("hello from client"))
	}()

	n, err = serverStream.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello from client" {
		t.Errorf("got %q", buf[:n])
	}
}

func TestHandshake_WrongPassword(t *testing.T) {
	side1, side2 := newPipePair()

	go func() {
		stream, err := ServerHandshake(side2, "correct")
		if err == nil {
			stream.Write([]byte("data")) // may block, that's ok — test goroutine will fail below
		}
	}()

	clientStream, err := ClientHandshake(side1, "wrong")
	if err != nil {
		t.Fatal(err) // Handshake itself succeeds (nonce exchange is unencrypted)
	}

	// Decryption should fail because keys differ
	buf := make([]byte, 1024)
	_, err = clientStream.Read(buf)
	if err == nil {
		t.Fatal("expected decryption error with wrong password")
	}
}
