//go:build windows

package bt

import "fmt"

// NewTransportWithProtocol creates a transport based on the protocol name.
// On Windows, only "rfcomm" is supported (L2CAP requires kernel-mode driver).
func NewTransportWithProtocol(proto string) Transport {
	if proto == "l2cap" {
		fmt.Println("Warning: L2CAP is not supported on Windows, falling back to RFCOMM")
	}
	return NewTransport() // RFCOMM
}
