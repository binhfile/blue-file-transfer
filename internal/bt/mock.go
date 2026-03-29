package bt

import (
	"io"
	"time"
)

// MockConn implements Conn using an in-memory pipe for testing.
type MockConn struct {
	reader     io.Reader
	writer     io.Writer
	remoteAddr string
	closed     bool
}

// NewMockConnPair creates a pair of connected MockConn instances.
// Data written to one can be read from the other.
func NewMockConnPair() (Conn, Conn) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	client := &MockConn{
		reader:     r1,
		writer:     w2,
		remoteAddr: "00:11:22:33:44:55",
	}
	server := &MockConn{
		reader:     r2,
		writer:     w1,
		remoteAddr: "AA:BB:CC:DD:EE:FF",
	}
	return client, server
}

func (c *MockConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

func (c *MockConn) Write(b []byte) (int, error) {
	return c.writer.Write(b)
}

func (c *MockConn) Close() error {
	c.closed = true
	if closer, ok := c.reader.(io.Closer); ok {
		closer.Close()
	}
	if closer, ok := c.writer.(io.Closer); ok {
		closer.Close()
	}
	return nil
}

func (c *MockConn) RemoteAddr() string {
	return c.remoteAddr
}

func (c *MockConn) SetDeadline(t time.Time) error    { return nil }
func (c *MockConn) SetReadDeadline(t time.Time) error { return nil }
func (c *MockConn) SetWriteDeadline(t time.Time) error { return nil }

// MockTransport implements Transport for testing.
type MockTransport struct {
	ListenerConn Conn // server side of the connection
	ClientConn   Conn // client side of the connection
}

func (t *MockTransport) Listen(adapter string, channel uint8) (Listener, error) {
	return &MockListener{conn: t.ListenerConn, addr: "00:11:22:33:44:55"}, nil
}

func (t *MockTransport) Connect(adapter string, remoteAddr string, channel uint8) (Conn, error) {
	return t.ClientConn, nil
}

func (t *MockTransport) Scan(adapter string, timeout time.Duration) ([]Device, error) {
	return []Device{
		{Address: "00:11:22:33:44:55", Name: "TestServer"},
	}, nil
}

// MockListener implements Listener for testing.
type MockListener struct {
	conn     Conn
	addr     string
	accepted bool
}

func (l *MockListener) Accept() (Conn, error) {
	if l.accepted {
		// Block forever after first accept (simulate waiting)
		select {}
	}
	l.accepted = true
	return l.conn, nil
}

func (l *MockListener) Close() error {
	return nil
}

func (l *MockListener) Addr() string {
	return l.addr
}
