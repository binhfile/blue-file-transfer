package bt

import (
	"io"
	"time"
)

// Device represents a discovered Bluetooth device.
type Device struct {
	Address string
	Name    string
}

// Conn represents a Bluetooth connection.
type Conn interface {
	io.ReadWriteCloser
	RemoteAddr() string
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// Listener accepts incoming Bluetooth connections.
type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() string
}

// Transport abstracts platform-specific Bluetooth RFCOMM operations.
type Transport interface {
	Listen(adapter string, channel uint8) (Listener, error)
	Connect(adapter string, remoteAddr string, channel uint8) (Conn, error)
	Scan(adapter string, timeout time.Duration) ([]Device, error)
}
