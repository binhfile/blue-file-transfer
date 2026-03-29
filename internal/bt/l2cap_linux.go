//go:build linux

package bt

import (
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// L2CAP constants not yet in golang.org/x/sys/unix
const (
	l2capOptions = 0x01 // L2CAP_OPTIONS setsockopt level

	// L2CAP modes
	l2capModeBasic = 0x00
	l2capModeERTM  = 0x03 // Enhanced Retransmission Mode

	// Default and maximum MTU values
	l2capDefaultMTU = 672
	l2capMaxMTU     = 65535

	// PSM range for dynamic/user protocols (odd numbers, 0x1001-0xFFFF)
	L2CAPDefaultPSM = 0x1001
)

// l2capOptions is the struct for L2CAP_OPTIONS setsockopt.
// Matches: struct l2cap_options { uint16 omtu; uint16 imtu; uint16 flush_to; uint8 mode; uint8 fcs; uint8 max_tx; uint16 txwin_size; }
type l2capOpts struct {
	OMTU      uint16
	IMTU      uint16
	FlushTo   uint16
	Mode      uint8
	FCS       uint8
	MaxTX     uint8
	TxWinSize uint16
}

// L2CAPTransport implements Transport using Linux L2CAP sockets.
// L2CAP operates below RFCOMM with lower overhead and higher throughput.
type L2CAPTransport struct {
	MTU uint16 // Requested MTU (0 = use max)
}

// NewL2CAPTransport creates a new L2CAP transport.
func NewL2CAPTransport() Transport {
	return &L2CAPTransport{MTU: l2capMaxMTU}
}

// channelToPSM converts a CLI channel number to an L2CAP PSM.
// PSM must be odd and >= 0x1001 for dynamic protocols.
func channelToPSM(channel uint8) uint16 {
	psm := L2CAPDefaultPSM + uint16(channel-1)*2
	// Ensure PSM is odd
	if psm%2 == 0 {
		psm++
	}
	return psm
}

// setL2CAPOptions sets the MTU on an L2CAP socket.
func setL2CAPOptions(fd int, mtu uint16) error {
	opts := l2capOpts{
		IMTU:    mtu,
		OMTU:    0, // read-only, set by remote
		FlushTo: 0xFFFF,
		Mode:    l2capModeBasic,
	}

	_, _, errno := unix.Syscall6(
		unix.SYS_SETSOCKOPT,
		uintptr(fd),
		uintptr(unix.SOL_L2CAP),
		uintptr(l2capOptions),
		uintptr(unsafe.Pointer(&opts)),
		unsafe.Sizeof(opts),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("setsockopt L2CAP_OPTIONS: %w", errno)
	}
	return nil
}

// getL2CAPOptions reads the negotiated MTU from an L2CAP socket.
func getL2CAPOptions(fd int) (*l2capOpts, error) {
	var opts l2capOpts
	optLen := uint32(unsafe.Sizeof(opts))

	_, _, errno := unix.Syscall6(
		unix.SYS_GETSOCKOPT,
		uintptr(fd),
		uintptr(unix.SOL_L2CAP),
		uintptr(l2capOptions),
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&optLen)),
		0,
	)
	if errno != 0 {
		return nil, fmt.Errorf("getsockopt L2CAP_OPTIONS: %w", errno)
	}
	return &opts, nil
}

// --- L2CAP Conn ---

type l2capConn struct {
	fd         int
	remoteAddr string
	file       *os.File
	mtu        uint16
}

func newL2CAPConn(fd int, remoteAddr string) *l2capConn {
	c := &l2capConn{
		fd:         fd,
		remoteAddr: remoteAddr,
		file:       os.NewFile(uintptr(fd), "l2cap"),
	}
	// Read negotiated MTU
	if opts, err := getL2CAPOptions(fd); err == nil {
		c.mtu = opts.OMTU
		if c.mtu == 0 {
			c.mtu = opts.IMTU
		}
	}
	return c
}

func (c *l2capConn) Read(b []byte) (int, error) {
	return c.file.Read(b)
}

func (c *l2capConn) Write(b []byte) (int, error) {
	// L2CAP SOCK_SEQPACKET preserves message boundaries.
	// For SOCK_STREAM, ensure full write.
	total := 0
	for total < len(b) {
		n, err := c.file.Write(b[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func (c *l2capConn) Close() error {
	return c.file.Close()
}

func (c *l2capConn) RemoteAddr() string {
	return c.remoteAddr
}

func (c *l2capConn) SetDeadline(t time.Time) error {
	return c.file.SetDeadline(t)
}

func (c *l2capConn) SetReadDeadline(t time.Time) error {
	return c.file.SetReadDeadline(t)
}

func (c *l2capConn) SetWriteDeadline(t time.Time) error {
	return c.file.SetWriteDeadline(t)
}

// --- L2CAP Listener ---

type l2capListener struct {
	fd   int
	addr string
	psm  uint16
}

func (l *l2capListener) Accept() (Conn, error) {
	nfd, sa, err := unix.Accept(l.fd)
	if err != nil {
		return nil, fmt.Errorf("accept: %w", err)
	}

	remoteAddr := "unknown"
	if rsa, ok := sa.(*unix.SockaddrL2); ok {
		remoteAddr = formatBTAddr(rsa.Addr)
	}

	_ = unix.SetsockoptInt(nfd, unix.SOL_SOCKET, unix.SO_SNDBUF, sockBufSize)
	_ = unix.SetsockoptInt(nfd, unix.SOL_SOCKET, unix.SO_RCVBUF, sockBufSize)

	return newL2CAPConn(nfd, remoteAddr), nil
}

func (l *l2capListener) Close() error {
	return unix.Close(l.fd)
}

func (l *l2capListener) Addr() string {
	return l.addr
}

// --- Transport methods ---

func (t *L2CAPTransport) Listen(adapter string, channel uint8) (Listener, error) {
	adapterAddr, err := resolveAdapter(adapter)
	if err != nil {
		return nil, fmt.Errorf("resolve adapter: %w", err)
	}

	// Use SOCK_STREAM for L2CAP — provides TCP-like stream semantics
	// which is compatible with the existing protocol layer.
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_L2CAP)
	if err != nil {
		return nil, fmt.Errorf("create L2CAP socket: %w", err)
	}

	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, sockBufSize)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, sockBufSize)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)

	// Request maximum MTU
	mtu := t.MTU
	if mtu == 0 {
		mtu = l2capMaxMTU
	}
	if err := setL2CAPOptions(fd, mtu); err != nil {
		// Non-fatal: continue with default MTU
		fmt.Printf("[l2cap] warning: MTU negotiation failed: %v (using default %d)\n", err, l2capDefaultMTU)
	}

	psm := channelToPSM(channel)
	sa := &unix.SockaddrL2{
		PSM:  psm,
		Addr: adapterAddr,
	}

	if err := unix.Bind(fd, sa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind L2CAP PSM %d: %w", psm, err)
	}

	if err := unix.Listen(fd, 1); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("listen: %w", err)
	}

	localAddr := formatBTAddr(adapterAddr)
	return &l2capListener{fd: fd, addr: localAddr, psm: psm}, nil
}

func (t *L2CAPTransport) Connect(adapter string, remoteAddr string, channel uint8) (Conn, error) {
	adapterAddr, err := resolveAdapter(adapter)
	if err != nil {
		return nil, fmt.Errorf("resolve adapter: %w", err)
	}

	targetAddr, err := parseBTAddr(remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("parse remote address: %w", err)
	}

	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_L2CAP)
	if err != nil {
		return nil, fmt.Errorf("create L2CAP socket: %w", err)
	}

	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, sockBufSize)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, sockBufSize)

	// Request maximum MTU
	mtu := t.MTU
	if mtu == 0 {
		mtu = l2capMaxMTU
	}
	_ = setL2CAPOptions(fd, mtu)

	// Bind to local adapter
	bindSa := &unix.SockaddrL2{
		PSM:  0,
		Addr: adapterAddr,
	}
	if err := unix.Bind(fd, bindSa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind to adapter: %w", err)
	}

	psm := channelToPSM(channel)
	connectSa := &unix.SockaddrL2{
		PSM:  psm,
		Addr: targetAddr,
	}
	if err := unix.Connect(fd, connectSa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("connect L2CAP PSM %d: %w", psm, err)
	}

	conn := newL2CAPConn(fd, remoteAddr)

	// Log negotiated MTU
	if opts, err := getL2CAPOptions(fd); err == nil {
		fmt.Printf("[l2cap] Connected: IMTU=%d OMTU=%d\n", opts.IMTU, opts.OMTU)
	}

	return conn, nil
}

func (t *L2CAPTransport) Scan(adapter string, timeout time.Duration) ([]Device, error) {
	// Scan is protocol-independent — reuse the same HCI scan
	return scanWithHcitool(adapter, timeout)
}
