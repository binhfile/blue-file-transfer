//go:build windows

package bt

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"
)

const (
	afBTH          = 32     // AF_BTH
	bthProtoRFCOMM = 0x0003 // BTHPROTO_RFCOMM

	sockBufSize = 65536

	// Winsock timeout socket options
	soRCVTIMEO = 0x1006 // SO_RCVTIMEO
	soSNDTIMEO = 0x1005 // SO_SNDTIMEO
)

var (
	modws2_32                    = syscall.NewLazyDLL("ws2_32.dll")
	procBind                     = modws2_32.NewProc("bind")
	procConnect                  = modws2_32.NewProc("connect")
	procSetsockopt               = modws2_32.NewProc("setsockopt")
	procWSAStartup               = modws2_32.NewProc("WSAStartup")
	procSend                     = modws2_32.NewProc("send")
	procRecv                     = modws2_32.NewProc("recv")
	procWSALookupServiceBeginW   = modws2_32.NewProc("WSALookupServiceBeginW")
	procWSALookupServiceNextW    = modws2_32.NewProc("WSALookupServiceNextW")
	procWSALookupServiceEnd      = modws2_32.NewProc("WSALookupServiceEnd")
)

func init() {
	// Initialize Winsock
	var wsaData [408]byte
	procWSAStartup.Call(uintptr(0x0202), uintptr(unsafe.Pointer(&wsaData[0])))
}

// sockaddrBTH is the Windows SOCKADDR_BTH structure (30 bytes).
type sockaddrBTH struct {
	AddressFamily  uint16
	BTAddr         uint64   // 6-byte BT address stored as uint64
	ServiceClassID [16]byte // GUID
	Port           uint32   // RFCOMM channel
}

type WindowsTransport struct{}

func NewTransport() Transport {
	return &WindowsTransport{}
}

func parseBTAddrWindows(addr string) (uint64, error) {
	var parts [6]byte
	n, err := fmt.Sscanf(addr, "%02x:%02x:%02x:%02x:%02x:%02x",
		&parts[0], &parts[1], &parts[2], &parts[3], &parts[4], &parts[5])
	if err != nil || n != 6 {
		return 0, fmt.Errorf("invalid bluetooth address: %s", addr)
	}

	var result uint64
	for i := 0; i < 6; i++ {
		result = (result << 8) | uint64(parts[i])
	}
	return result, nil
}

func formatBTAddrWindows(addr uint64) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		(addr>>40)&0xFF, (addr>>32)&0xFF, (addr>>24)&0xFF,
		(addr>>16)&0xFF, (addr>>8)&0xFF, addr&0xFF)
}

func marshalSockaddrBTH(sa *sockaddrBTH) [30]byte {
	var buf [30]byte
	binary.LittleEndian.PutUint16(buf[0:2], sa.AddressFamily)
	binary.LittleEndian.PutUint64(buf[2:10], sa.BTAddr)
	copy(buf[10:26], sa.ServiceClassID[:])
	binary.LittleEndian.PutUint32(buf[26:30], sa.Port)
	return buf
}

// rawBind calls ws2_32.bind directly with raw sockaddr bytes.
func rawBind(fd syscall.Handle, sa *[30]byte) error {
	r1, _, e1 := procBind.Call(
		uintptr(fd),
		uintptr(unsafe.Pointer(&sa[0])),
		uintptr(30),
	)
	if r1 != 0 {
		return fmt.Errorf("bind: %w", e1)
	}
	return nil
}

// rawConnect calls ws2_32.connect directly with raw sockaddr bytes.
func rawConnect(fd syscall.Handle, sa *[30]byte) error {
	r1, _, e1 := procConnect.Call(
		uintptr(fd),
		uintptr(unsafe.Pointer(&sa[0])),
		uintptr(30),
	)
	if r1 != 0 {
		return fmt.Errorf("connect: %w", e1)
	}
	return nil
}

// setSocketTimeout sets a timeout on a socket using raw setsockopt.
func setSocketTimeout(fd syscall.Handle, optName int, timeout time.Duration) error {
	ms := int32(0)
	if timeout > 0 {
		ms = int32(timeout.Milliseconds())
	}
	r1, _, e1 := procSetsockopt.Call(
		uintptr(fd),
		uintptr(syscall.SOL_SOCKET),
		uintptr(optName),
		uintptr(unsafe.Pointer(&ms)),
		uintptr(4),
	)
	if r1 != 0 {
		return fmt.Errorf("setsockopt: %w", e1)
	}
	return nil
}

type winsockConn struct {
	fd         syscall.Handle
	remoteAddr string
}

func (c *winsockConn) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	// Use Winsock recv() — syscall.Read() calls ReadFile which doesn't work on sockets
	r1, _, e1 := procRecv.Call(
		uintptr(c.fd),
		uintptr(unsafe.Pointer(&b[0])),
		uintptr(len(b)),
		0, // flags
	)
	n := int(r1)
	if int32(r1) == -1 { // SOCKET_ERROR
		return 0, fmt.Errorf("recv: %w", e1)
	}
	if n == 0 {
		return 0, fmt.Errorf("recv: connection closed")
	}
	return n, nil
}

func (c *winsockConn) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	// Use Winsock send() — syscall.Write() calls WriteFile which doesn't work on sockets
	total := 0
	for total < len(b) {
		chunk := b[total:]
		r1, _, e1 := procSend.Call(
			uintptr(c.fd),
			uintptr(unsafe.Pointer(&chunk[0])),
			uintptr(len(chunk)),
			0, // flags
		)
		n := int(r1)
		if int32(r1) == -1 { // SOCKET_ERROR
			return total, fmt.Errorf("send: %w", e1)
		}
		total += n
	}
	return total, nil
}

func (c *winsockConn) Close() error {
	return syscall.Closesocket(c.fd)
}

func (c *winsockConn) RemoteAddr() string {
	return c.remoteAddr
}

func (c *winsockConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func (c *winsockConn) SetReadDeadline(t time.Time) error {
	d := time.Duration(0)
	if !t.IsZero() {
		d = time.Until(t)
		if d < 0 {
			d = 0
		}
	}
	return setSocketTimeout(c.fd, soRCVTIMEO, d)
}

func (c *winsockConn) SetWriteDeadline(t time.Time) error {
	d := time.Duration(0)
	if !t.IsZero() {
		d = time.Until(t)
		if d < 0 {
			d = 0
		}
	}
	return setSocketTimeout(c.fd, soSNDTIMEO, d)
}

type winsockListener struct {
	fd      syscall.Handle
	addr    string
	channel uint8
}

func (l *winsockListener) Accept() (Conn, error) {
	nfd, _, err := syscall.Accept(l.fd)
	if err != nil {
		return nil, fmt.Errorf("accept: %w", err)
	}

	_ = syscall.SetsockoptInt(nfd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, sockBufSize)
	_ = syscall.SetsockoptInt(nfd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, sockBufSize)

	return &winsockConn{fd: nfd, remoteAddr: "unknown"}, nil
}

func (l *winsockListener) Close() error {
	return syscall.Closesocket(l.fd)
}

func (l *winsockListener) Addr() string {
	return l.addr
}

func (t *WindowsTransport) Listen(adapter string, channel uint8) (Listener, error) {
	fd, err := syscall.Socket(afBTH, syscall.SOCK_STREAM, bthProtoRFCOMM)
	if err != nil {
		return nil, fmt.Errorf("create socket: %w", err)
	}

	_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, sockBufSize)
	_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, sockBufSize)

	sa := marshalSockaddrBTH(&sockaddrBTH{
		AddressFamily: uint16(afBTH),
		BTAddr:        0, // Any adapter
		Port:          uint32(channel),
	})

	if err := rawBind(fd, &sa); err != nil {
		syscall.Closesocket(fd)
		return nil, err
	}

	if err := syscall.Listen(fd, 1); err != nil {
		syscall.Closesocket(fd)
		return nil, fmt.Errorf("listen: %w", err)
	}

	return &winsockListener{fd: fd, addr: adapter, channel: channel}, nil
}

func (t *WindowsTransport) Connect(adapter string, remoteAddr string, channel uint8) (Conn, error) {
	fd, err := syscall.Socket(afBTH, syscall.SOCK_STREAM, bthProtoRFCOMM)
	if err != nil {
		return nil, fmt.Errorf("create socket: %w", err)
	}

	_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, sockBufSize)
	_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, sockBufSize)

	btAddr, err := parseBTAddrWindows(remoteAddr)
	if err != nil {
		syscall.Closesocket(fd)
		return nil, err
	}

	sa := marshalSockaddrBTH(&sockaddrBTH{
		AddressFamily: uint16(afBTH),
		BTAddr:        btAddr,
		Port:          uint32(channel),
	})

	if err := rawConnect(fd, &sa); err != nil {
		syscall.Closesocket(fd)
		return nil, err
	}

	return &winsockConn{fd: fd, remoteAddr: remoteAddr}, nil
}

// --- WSALookupService structures for Bluetooth device discovery ---

// WSAQUERYSET flags
const (
	lUP_CONTAINERS       = 0x0002
	lUP_RETURN_NAME      = 0x0010
	lUP_RETURN_ADDR      = 0x0100
	lUP_FLUSHCACHE       = 0x1000
	lUP_RETURN_TYPE      = 0x0020
	lUP_RETURN_BLOB      = 0x0200
	lUP_RES_SERVICE      = 0x8000

	nsNLA               = 15  // NS_NLA
	nsBTH               = 16  // NS_BTH

	wsaENoMore          = 10110
)

// GUID for Bluetooth L2CAP Protocol
var l2capProtocolUUID = [16]byte{
	0x00, 0x00, 0x01, 0x00, // 0x0100
	0x00, 0x00,
	0x10, 0x00,
	0x80, 0x00,
	0x00, 0x80, 0x5F, 0x9B, 0x34, 0xFB,
}

// wsaQuerySetW is the WSAQUERYSETW structure (Windows wide-char version).
// This must match the exact Windows layout.
type wsaQuerySetW struct {
	Size                uint32
	ServiceInstanceName *uint16
	ServiceClassID      *[16]byte
	Version             *byte // WSAVERSION*
	Comment             *uint16
	NameSpace           uint32
	NSProviderID        *[16]byte
	Context             *uint16
	NumberOfProtocols   uint32
	AfpProtocols        uintptr
	QueryString         *uint16
	NumberOfCsAddrs     uint32
	CsaBuffer           uintptr
	OutputFlags         uint32
	Blob                uintptr
}

// csaddrInfo is CSADDR_INFO structure.
type csaddrInfo struct {
	LocalAddr  socketAddressInfo
	RemoteAddr socketAddressInfo
	SocketType int32
	Protocol   int32
}

// socketAddressInfo is SOCKET_ADDRESS.
type socketAddressInfo struct {
	Sockaddr       uintptr
	SockaddrLength int32
}

func (t *WindowsTransport) Scan(adapter string, timeout time.Duration) ([]Device, error) {
	// Set up query for Bluetooth device inquiry
	var qs wsaQuerySetW
	qs.Size = uint32(unsafe.Sizeof(qs))
	qs.NameSpace = nsBTH

	flags := uint32(lUP_CONTAINERS | lUP_RETURN_NAME | lUP_RETURN_ADDR | lUP_FLUSHCACHE)

	var hLookup syscall.Handle
	r1, _, e1 := procWSALookupServiceBeginW.Call(
		uintptr(unsafe.Pointer(&qs)),
		uintptr(flags),
		uintptr(unsafe.Pointer(&hLookup)),
	)
	if r1 != 0 {
		return nil, fmt.Errorf("WSALookupServiceBegin: %w", e1)
	}
	defer procWSALookupServiceEnd.Call(uintptr(hLookup))

	var devices []Device
	buf := make([]byte, 4096)

	for {
		bufLen := uint32(len(buf))
		r1, _, e1 := procWSALookupServiceNextW.Call(
			uintptr(hLookup),
			uintptr(flags),
			uintptr(unsafe.Pointer(&bufLen)),
			uintptr(unsafe.Pointer(&buf[0])),
		)
		if r1 != 0 {
			// Check if it's the "no more results" error
			errno, ok := e1.(syscall.Errno)
			if ok && uint32(errno) == wsaENoMore {
				break
			}
			// Try with larger buffer
			if bufLen > uint32(len(buf)) {
				buf = make([]byte, bufLen)
				continue
			}
			break
		}

		// Parse the WSAQUERYSETW result from buf
		result := (*wsaQuerySetW)(unsafe.Pointer(&buf[0]))

		dev := Device{}

		// Extract device name (wide string)
		if result.ServiceInstanceName != nil {
			dev.Name = utf16PtrToString(result.ServiceInstanceName)
		}

		// Extract BT address from CSADDR_INFO
		if result.NumberOfCsAddrs > 0 && result.CsaBuffer != 0 {
			csaddr := (*csaddrInfo)(unsafe.Pointer(result.CsaBuffer))
			if csaddr.RemoteAddr.SockaddrLength >= 30 && csaddr.RemoteAddr.Sockaddr != 0 {
				// Read SOCKADDR_BTH from remote address
				saBytes := (*[30]byte)(unsafe.Pointer(csaddr.RemoteAddr.Sockaddr))
				btAddr := binary.LittleEndian.Uint64(saBytes[2:10])
				dev.Address = formatBTAddrWindows(btAddr)
			}
		}

		if dev.Address == "" && dev.Name != "" {
			dev.Address = "unknown"
		}

		if dev.Address != "" {
			if dev.Name == "" {
				dev.Name = dev.Address
			}
			devices = append(devices, dev)
		}
	}

	return devices, nil
}

// utf16PtrToString converts a *uint16 (null-terminated UTF-16) to Go string.
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	// Find length
	ptr := unsafe.Pointer(p)
	var chars []uint16
	for i := 0; ; i++ {
		c := *(*uint16)(unsafe.Pointer(uintptr(ptr) + uintptr(i)*2))
		if c == 0 {
			break
		}
		chars = append(chars, c)
		if i > 256 { // safety limit
			break
		}
	}
	return syscall.UTF16ToString(chars)
}

// ListAdapters returns available Bluetooth adapters on Windows.
// Windows typically has a single "default" Bluetooth radio.
func ListAdapters() ([]string, error) {
	// On Windows, the default adapter is used automatically.
	// Return a placeholder name.
	return []string{"default"}, nil
}

// Ensure net is used
var _ = net.Dial
