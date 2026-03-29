//go:build linux

package bt

// NewTransportWithProtocol creates a transport based on the protocol name.
// Supported: "rfcomm" (default), "l2cap" (Linux only, higher throughput).
func NewTransportWithProtocol(proto string) Transport {
	if proto == "l2cap" {
		return NewL2CAPTransport()
	}
	return NewTransport() // RFCOMM
}
