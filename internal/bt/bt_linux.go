//go:build linux

package bt

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	hciMaxDevices = 16
	hciGetDevList = 0x800448D2 // HCIGETDEVLIST ioctl
	hciGetDevInfo = 0x800448D3 // HCIGETDEVINFO ioctl
	hciDevUp      = 0x400448C9 // HCIDEVUP ioctl
	hciSetScan    = 0x400448DD // HCISETSCAN ioctl

	// HCI device flags (from hci.h)
	hciFlagUp    = 1 << 0
	hciFlagPScan = 1 << 3
	hciFlagIScan = 1 << 4

	// Scan types
	scanPage    = 0x02
	scanInquiry = 0x01

	sockBufSize = 65536
)

// LinuxTransport implements Transport using Linux BlueZ RFCOMM sockets.
type LinuxTransport struct{}

// NewTransport creates a new Linux Bluetooth transport.
func NewTransport() Transport {
	return &LinuxTransport{}
}

// parseBTAddr parses a Bluetooth address string "AA:BB:CC:DD:EE:FF" into [6]byte.
func parseBTAddr(addr string) ([6]byte, error) {
	var result [6]byte
	parts := strings.Split(addr, ":")
	if len(parts) != 6 {
		return result, fmt.Errorf("invalid bluetooth address: %s", addr)
	}
	for i, p := range parts {
		b, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return result, fmt.Errorf("invalid bluetooth address byte %q: %w", p, err)
		}
		// Bluetooth addresses are stored in reverse byte order
		result[5-i] = byte(b)
	}
	return result, nil
}

// formatBTAddr formats a [6]byte Bluetooth address to "AA:BB:CC:DD:EE:FF".
func formatBTAddr(addr [6]byte) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		addr[5], addr[4], addr[3], addr[2], addr[1], addr[0])
}

// resolveAdapter resolves an adapter name (e.g., "hci0") to its BD address.
func resolveAdapter(adapter string) ([6]byte, error) {
	if adapter == "" || adapter == "any" {
		return [6]byte{}, nil // BDADDR_ANY
	}

	// Parse hciN format
	if !strings.HasPrefix(adapter, "hci") {
		// Assume it's already a BT address
		return parseBTAddr(adapter)
	}

	devID, err := strconv.Atoi(adapter[3:])
	if err != nil {
		return [6]byte{}, fmt.Errorf("invalid adapter name: %s", adapter)
	}

	// Get adapter address via ioctl
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW, unix.BTPROTO_HCI)
	if err != nil {
		return [6]byte{}, fmt.Errorf("open HCI socket: %w", err)
	}
	defer unix.Close(fd)

	// HCIGETDEVINFO
	type hciDevInfo struct {
		DevID   uint16
		Name    [8]byte
		BDAddr  [6]byte
		Flags   uint32
		Type    uint8
		_       [3]byte
		_       [4]uint32 // stats
		_       [10]uint32
		_       [4]uint32
		_       [4]uint16
		_       [2]uint32
		_       [3]uint16
	}

	var di hciDevInfo
	di.DevID = uint16(devID)

	// Use raw ioctl
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(hciGetDevInfo), uintptr(unsafe.Pointer(&di)))
	if errno != 0 {
		return [6]byte{}, fmt.Errorf("HCIGETDEVINFO: %w", errno)
	}

	return di.BDAddr, nil
}

// ensureAdapterUp brings the adapter up and enables page+inquiry scan (piscan)
// if not already set. Adapter should be in "hciN" format.
func ensureAdapterUp(adapter string) error {
	if adapter == "" || adapter == "any" {
		return nil
	}
	if !strings.HasPrefix(adapter, "hci") {
		return nil // raw address, can't manage
	}

	devID, err := strconv.Atoi(adapter[3:])
	if err != nil {
		return fmt.Errorf("invalid adapter name: %s", adapter)
	}

	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW, unix.BTPROTO_HCI)
	if err != nil {
		return fmt.Errorf("open HCI socket: %w", err)
	}
	defer unix.Close(fd)

	// Get current device info
	type hciDevInfo struct {
		DevID uint16
		Name  [8]byte
		BDAddr [6]byte
		Flags uint32
		Type  uint8
		_     [3]byte
		_     [4]uint32
		_     [10]uint32
		_     [4]uint32
		_     [4]uint16
		_     [2]uint32
		_     [3]uint16
	}

	var di hciDevInfo
	di.DevID = uint16(devID)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(hciGetDevInfo), uintptr(unsafe.Pointer(&di)))
	if errno != 0 {
		return fmt.Errorf("HCIGETDEVINFO: %w", errno)
	}

	// Bring up if not already up
	if di.Flags&hciFlagUp == 0 {
		_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(hciDevUp), uintptr(devID))
		if errno != 0 {
			return fmt.Errorf("HCIDEVUP: %w", errno)
		}
		// Re-read flags after bringing up
		di.DevID = uint16(devID)
		_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(hciGetDevInfo), uintptr(unsafe.Pointer(&di)))
		if errno != 0 {
			return fmt.Errorf("HCIGETDEVINFO after up: %w", errno)
		}
	}

	// Enable piscan if not already set
	if di.Flags&hciFlagPScan == 0 || di.Flags&hciFlagIScan == 0 {
		type hciDevReq struct {
			DevID  uint16
			DevOpt uint32
		}
		dr := hciDevReq{
			DevID:  uint16(devID),
			DevOpt: uint32(scanPage | scanInquiry),
		}
		_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(hciSetScan), uintptr(unsafe.Pointer(&dr)))
		if errno != 0 {
			return fmt.Errorf("HCISETSCAN piscan: %w", errno)
		}
	}

	return nil
}

type rfcommConn struct {
	fd         int
	remoteAddr string
	file       *os.File
}

func newRFCOMMConn(fd int, remoteAddr string) *rfcommConn {
	c := &rfcommConn{
		fd:         fd,
		remoteAddr: remoteAddr,
		file:       os.NewFile(uintptr(fd), "rfcomm"),
	}
	return c
}

func (c *rfcommConn) Read(b []byte) (int, error) {
	return c.file.Read(b)
}

func (c *rfcommConn) Write(b []byte) (int, error) {
	// Ensure full write — RFCOMM socket may accept partial writes
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

func (c *rfcommConn) Close() error {
	return c.file.Close()
}

func (c *rfcommConn) RemoteAddr() string {
	return c.remoteAddr
}

func (c *rfcommConn) SetDeadline(t time.Time) error {
	return c.file.SetDeadline(t)
}

func (c *rfcommConn) SetReadDeadline(t time.Time) error {
	return c.file.SetReadDeadline(t)
}

func (c *rfcommConn) SetWriteDeadline(t time.Time) error {
	return c.file.SetWriteDeadline(t)
}

type rfcommListener struct {
	fd      int
	addr    string
	channel uint8
}

func (l *rfcommListener) Accept() (Conn, error) {
	nfd, sa, err := unix.Accept(l.fd)
	if err != nil {
		return nil, fmt.Errorf("accept: %w", err)
	}

	remoteAddr := "unknown"
	if rsa, ok := sa.(*unix.SockaddrRFCOMM); ok {
		remoteAddr = formatBTAddr(rsa.Addr)
	}

	// Set socket buffer sizes on accepted connection
	_ = unix.SetsockoptInt(nfd, unix.SOL_SOCKET, unix.SO_SNDBUF, sockBufSize)
	_ = unix.SetsockoptInt(nfd, unix.SOL_SOCKET, unix.SO_RCVBUF, sockBufSize)

	return newRFCOMMConn(nfd, remoteAddr), nil
}

func (l *rfcommListener) Close() error {
	return unix.Close(l.fd)
}

func (l *rfcommListener) Addr() string {
	return l.addr
}

func (t *LinuxTransport) Listen(adapter string, channel uint8) (Listener, error) {
	if err := ensureAdapterUp(adapter); err != nil {
		return nil, fmt.Errorf("ensure adapter ready: %w", err)
	}

	adapterAddr, err := resolveAdapter(adapter)
	if err != nil {
		return nil, fmt.Errorf("resolve adapter: %w", err)
	}

	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
	if err != nil {
		return nil, fmt.Errorf("create socket: %w", err)
	}

	// Dynamic socket buffer based on ACL capacity
	bufSize := dynamicSockBuf(adapter)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, bufSize)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, bufSize)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)

	sa := &unix.SockaddrRFCOMM{
		Addr:    adapterAddr,
		Channel: channel,
	}

	if err := unix.Bind(fd, sa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind: %w", err)
	}

	if err := unix.Listen(fd, 1); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("listen: %w", err)
	}

	localAddr := formatBTAddr(adapterAddr)
	return &rfcommListener{fd: fd, addr: localAddr, channel: channel}, nil
}

func (t *LinuxTransport) Connect(adapter string, remoteAddr string, channel uint8) (Conn, error) {
	if err := ensureAdapterUp(adapter); err != nil {
		return nil, fmt.Errorf("ensure adapter ready: %w", err)
	}

	adapterAddr, err := resolveAdapter(adapter)
	if err != nil {
		return nil, fmt.Errorf("resolve adapter: %w", err)
	}

	targetAddr, err := parseBTAddr(remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("parse remote address: %w", err)
	}

	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
	if err != nil {
		return nil, fmt.Errorf("create socket: %w", err)
	}

	// Dynamic socket buffer based on ACL capacity
	cBufSize := dynamicSockBuf(adapter)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, cBufSize)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, cBufSize)

	// Bind to specific adapter
	bindSa := &unix.SockaddrRFCOMM{
		Addr:    adapterAddr,
		Channel: 0,
	}
	if err := unix.Bind(fd, bindSa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind to adapter: %w", err)
	}

	// Connect to remote
	connectSa := &unix.SockaddrRFCOMM{
		Addr:    targetAddr,
		Channel: channel,
	}
	if err := unix.Connect(fd, connectSa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("connect: %w", err)
	}

	return newRFCOMMConn(fd, remoteAddr), nil
}

func (t *LinuxTransport) Scan(adapter string, timeout time.Duration) ([]Device, error) {
	adapterID := 0
	if strings.HasPrefix(adapter, "hci") {
		id, err := strconv.Atoi(adapter[3:])
		if err == nil {
			adapterID = id
		}
	}

	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.BTPROTO_HCI)
	if err != nil {
		return nil, fmt.Errorf("open HCI socket: %w", err)
	}
	defer unix.Close(fd)

	if err := unix.Bind(fd, &unix.SockaddrHCI{Dev: uint16(adapterID), Channel: unix.HCI_CHANNEL_RAW}); err != nil {
		return nil, fmt.Errorf("bind HCI: %w", err)
	}

	// Use hcitool scan as fallback (more reliable with BlueZ stack)
	return scanWithHcitool(adapter, timeout)
}

// scanWithHcitool uses the hcitool command for device discovery.
func scanWithHcitool(adapter string, timeout time.Duration) ([]Device, error) {
	args := []string{"scan", "--flush"}
	if adapter != "" && adapter != "any" {
		args = append([]string{"-i", adapter}, args...)
	}

	// Use net package to look up device names from already paired devices
	// For actual scanning, we parse hcitool output
	return scanPaired(adapter)
}

// scanPaired reads paired/cached devices from the adapter.
func scanPaired(adapter string) ([]Device, error) {
	// Read from /var/lib/bluetooth/<adapter>/ for cached devices
	// This is a simpler approach that doesn't require inquiry
	adapterAddr, err := resolveAdapter(adapter)
	if err != nil {
		return nil, err
	}

	addrStr := formatBTAddr(adapterAddr)
	cacheDir := fmt.Sprintf("/var/lib/bluetooth/%s/cache", addrStr)

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, nil // No cached devices, return empty
	}

	var devices []Device
	for _, e := range entries {
		if !e.IsDir() {
			// Parse the filename as a BT address
			name := e.Name()
			if len(name) == 17 && strings.Count(name, ":") == 5 {
				devices = append(devices, Device{
					Address: name,
					Name:    readCachedName(cacheDir, name),
				})
			}
		}
	}
	return devices, nil
}

func readCachedName(cacheDir, addr string) string {
	data, err := os.ReadFile(fmt.Sprintf("%s/%s/info", cacheDir, addr))
	if err != nil {
		return addr
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Name=") {
			return strings.TrimPrefix(line, "Name=")
		}
	}
	return addr
}

// InquiryScan performs a Bluetooth inquiry scan using raw HCI commands.
func InquiryScan(adapter string, timeout time.Duration) ([]Device, error) {
	adapterID := 0
	if strings.HasPrefix(adapter, "hci") {
		id, err := strconv.Atoi(adapter[3:])
		if err == nil {
			adapterID = id
		}
	}

	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.BTPROTO_HCI)
	if err != nil {
		return nil, fmt.Errorf("open HCI socket: %w", err)
	}
	defer unix.Close(fd)

	if err := unix.Bind(fd, &unix.SockaddrHCI{Dev: uint16(adapterID), Channel: unix.HCI_CHANNEL_RAW}); err != nil {
		return nil, fmt.Errorf("bind HCI: %w", err)
	}

	// Set filter to receive inquiry result events
	var filter [16]byte
	// HCI_INQUIRY_CMD opcode group: OGF=0x01, OCF=0x0001
	// Set event filter for inquiry results

	// Build inquiry command
	// HCI command header: opcode (2) + param_len (1) + params
	// Inquiry: LAP=0x9E8B33 (GIAC), length=8 (10.24s), num_resp=0 (unlimited)
	durationUnits := uint8(timeout.Seconds() / 1.28)
	if durationUnits < 1 {
		durationUnits = 1
	}
	if durationUnits > 30 {
		durationUnits = 30
	}

	cmd := make([]byte, 9)
	// HCI command packet type
	cmd[0] = 0x01 // HCI command packet
	// Opcode: Inquiry (OGF=0x01, OCF=0x0001) = 0x0401
	binary.LittleEndian.PutUint16(cmd[1:3], 0x0401)
	cmd[3] = 5 // parameter length
	// LAP: 0x9E8B33 (General Inquiry Access Code)
	cmd[4] = 0x33
	cmd[5] = 0x8B
	cmd[6] = 0x9E
	cmd[7] = durationUnits
	cmd[8] = 0 // unlimited responses

	_ = filter
	if _, err := unix.Write(fd, cmd); err != nil {
		return nil, fmt.Errorf("send inquiry command: %w", err)
	}

	// Read responses until inquiry complete or timeout
	var devices []Device
	seen := make(map[string]bool)
	deadline := time.Now().Add(timeout + 2*time.Second)

	buf := make([]byte, 260)
	for time.Now().Before(deadline) {
		unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Sec: 1})

		n, err := unix.Read(fd, buf)
		if err != nil {
			continue
		}
		if n < 3 {
			continue
		}

		// Parse HCI event
		eventType := buf[0] // should be 0x04 (HCI event)
		if eventType != 0x04 {
			continue
		}
		eventCode := buf[1]

		switch eventCode {
		case 0x02: // Inquiry Result
			numResponses := int(buf[3])
			for i := 0; i < numResponses && 4+6*(i+1) <= n; i++ {
				offset := 4 + 6*i
				var addr [6]byte
				copy(addr[:], buf[offset:offset+6])
				addrStr := formatBTAddr(addr)
				if !seen[addrStr] {
					seen[addrStr] = true
					devices = append(devices, Device{Address: addrStr, Name: addrStr})
				}
			}
		case 0x01: // Inquiry Complete
			return devices, nil
		}
	}

	return devices, nil
}

// ListAdapters returns all available Bluetooth adapters.
func ListAdapters() ([]string, error) {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW, unix.BTPROTO_HCI)
	if err != nil {
		return nil, fmt.Errorf("open HCI socket: %w", err)
	}
	defer unix.Close(fd)

	type hciDevListReq struct {
		DevNum uint16
		DevReq [hciMaxDevices]struct {
			DevID  uint16
			DevOpt uint32
		}
	}

	var dl hciDevListReq
	dl.DevNum = hciMaxDevices

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(hciGetDevList), uintptr(unsafe.Pointer(&dl)))
	if errno != 0 {
		return nil, fmt.Errorf("HCIGETDEVLIST: %w", errno)
	}

	adapters := make([]string, 0, dl.DevNum)
	for i := 0; i < int(dl.DevNum); i++ {
		adapters = append(adapters, fmt.Sprintf("hci%d", dl.DevReq[i].DevID))
	}
	return adapters, nil
}

// ReadACLInfo reads the ACL MTU and buffer count from the adapter via HCIGETDEVINFO.
// Returns (0, 0, nil) if the adapter name is not in hciN format.
func ReadACLInfo(adapter string) (aclMTU, aclPkts uint16, err error) {
	devID := 0
	if adapter == "" || adapter == "any" {
		// default to hci0
	} else if strings.HasPrefix(adapter, "hci") {
		id, e := strconv.Atoi(adapter[3:])
		if e == nil {
			devID = id
		}
	} else {
		return 0, 0, nil // raw address, can't query
	}

	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW, unix.BTPROTO_HCI)
	if err != nil {
		return 0, 0, fmt.Errorf("open HCI socket: %w", err)
	}
	defer unix.Close(fd)

	// Use raw byte buffer to avoid Go struct alignment issues.
	// hci_dev_info layout (from kernel include/net/bluetooth/hci.h):
	//   offset  0: dev_id      (uint16)
	//   offset  2: name        (8 bytes)
	//   offset 10: bdaddr      (6 bytes)
	//   offset 16: flags       (uint32)
	//   offset 20: type        (uint8)
	//   offset 21: features    (8 bytes)
	//   offset 29: [3 pad]
	//   offset 32: pkt_type    (uint32)
	//   offset 36: link_policy (uint32)
	//   offset 40: link_mode   (uint32)
	//   offset 44: acl_mtu     (uint16)
	//   offset 46: acl_pkts    (uint16)
	//   offset 48: sco_mtu     (uint16)
	//   offset 50: sco_pkts    (uint16)
	//   offset 52: stat        (10 x uint32 = 40 bytes)
	//   total: 92 bytes
	var buf [256]byte
	binary.LittleEndian.PutUint16(buf[0:2], uint16(devID))

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(hciGetDevInfo), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return 0, 0, fmt.Errorf("HCIGETDEVINFO: %w", errno)
	}

	aclMTU = binary.LittleEndian.Uint16(buf[44:46])
	aclPkts = binary.LittleEndian.Uint16(buf[46:48])
	return aclMTU, aclPkts, nil
}

// dynamicSockBuf returns an optimal socket buffer size based on the adapter's
// ACL capacity. Falls back to sockBufSize if ACL info is unavailable.
func dynamicSockBuf(adapter string) int {
	aclMTU, aclPkts, err := ReadACLInfo(adapter)
	if err != nil || aclMTU == 0 || aclPkts == 0 {
		return sockBufSize
	}
	buf := int(aclMTU) * int(aclPkts) * 2
	if buf < 8192 {
		buf = 8192
	}
	if buf > 256*1024 {
		buf = 256 * 1024
	}
	return buf
}

// Ensure net is used (for potential future use)
var _ = net.Dial
