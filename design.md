# Blue File Transfer - Design

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      CLI Layer                          │
│  (cobra commands: server, client, scan)                 │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                   Protocol Layer                        │
│  (message encoding/decoding, request/response routing)  │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                  Transfer Engine                        │
│  (chunked I/O, checksums, progress, resume)             │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│              Bluetooth Transport Layer                  │
│  (platform-abstracted RFCOMM socket interface)          │
└──────────────────────┬──────────────────────────────────┘
                       │
         ┌─────────────┴─────────────┐
         │                           │
┌────────▼────────┐       ┌──────────▼────────┐
│  bt_linux.go    │       │  bt_windows.go    │
│  AF_BLUETOOTH   │       │  AF_BTH (0x20)    │
│  BTPROTO_RFCOMM │       │  BTHPROTO_RFCOMM  │
│  unix.Sockaddr  │       │  syscall.Socket   │
│  HCI device sel │       │  Winsock2 raw     │
└─────────────────┘       └───────────────────┘
```

## Project Structure

```
blue-file-transfer/
├── cmd/
│   └── bft/
│       └── main.go              # Entry point
├── internal/
│   ├── bt/
│   │   ├── transport.go         # Transport interface
│   │   ├── bt_linux.go          # Linux RFCOMM implementation
│   │   ├── bt_windows.go        # Windows RFCOMM implementation
│   │   ├── bt_linux_test.go     # Linux-specific tests
│   │   ├── bt_windows_test.go   # Windows-specific tests
│   │   ├── discovery.go         # Device discovery interface
│   │   ├── discovery_linux.go   # Linux HCI scan
│   │   └── discovery_windows.go # Windows Winsock scan
│   ├── protocol/
│   │   ├── message.go           # Message types and encoding
│   │   ├── message_test.go      # Message encoding/decoding tests
│   │   ├── handler.go           # Server-side request handler
│   │   └── handler_test.go      # Handler unit tests
│   ├── transfer/
│   │   ├── engine.go            # Chunked transfer engine
│   │   ├── engine_test.go       # Transfer engine tests
│   │   ├── checksum.go          # CRC32 computation
│   │   └── checksum_test.go     # Checksum tests
│   ├── server/
│   │   ├── server.go            # Server logic
│   │   └── server_test.go       # Server tests
│   ├── client/
│   │   ├── client.go            # Client logic
│   │   ├── client_test.go       # Client tests
│   │   └── cli.go               # Interactive CLI
│   └── fsutil/
│       ├── pathutil.go          # Path sanitization, traversal prevention
│       ├── pathutil_test.go     # Path security tests
│       ├── fileops.go           # File operations (ls, cp, mv, rm, mkdir)
│       └── fileops_test.go      # File operations tests
├── Makefile
├── go.mod
├── go.sum
├── requirement.md
├── design.md
└── plan.md
```

## Bluetooth Transport Layer

### Interface

```go
// Transport abstracts platform-specific Bluetooth RFCOMM operations.
type Transport interface {
    // Server operations
    Listen(adapter string, channel uint8) (Listener, error)

    // Client operations
    Connect(adapter string, remoteAddr string, channel uint8) (Conn, error)

    // Discovery
    Scan(adapter string, timeout time.Duration) ([]Device, error)
}

type Listener interface {
    Accept() (Conn, error)
    Close() error
    Addr() string // Local Bluetooth address
}

type Conn interface {
    io.ReadWriteCloser
    RemoteAddr() string
    SetDeadline(t time.Time) error
}

type Device struct {
    Address string
    Name    string
}
```

### Linux Implementation (bt_linux.go)

Uses `golang.org/x/sys/unix` — no CGO required.

```go
// Key syscalls:
fd, _ := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
unix.Bind(fd, &unix.SockaddrRFCOMM{Channel: channel, Addr: localAddr})
unix.Listen(fd, 1)
unix.Accept(fd)
unix.Connect(fd, &unix.SockaddrRFCOMM{Channel: channel, Addr: remoteAddr})
```

**Adapter selection**: Bind to a specific adapter by using its BD address in the `SockaddrRFCOMM.Addr` field. Use `[0, 0, 0, 0, 0, 0]` for "any adapter".

**Socket options for performance**:
```go
// Set socket send/receive buffer sizes
unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, 65536)
unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 65536)
```

### Windows Implementation (bt_windows.go)

Uses raw syscalls — no CGO required.

```go
const (
    AF_BTH         = 32
    BTHPROTO_RFCOMM = 3
)

// SOCKADDR_BTH layout (30 bytes):
// addressFamily uint16 (offset 0)
// btAddr        uint64 (offset 2) - 6-byte BT address as uint64
// serviceClassId GUID  (offset 10) - 16 bytes
// port          uint32 (offset 26) - RFCOMM channel
type sockaddrBTH struct {
    Family         uint16
    BTAddr         uint64
    ServiceClassID [16]byte
    Port           uint32
}

fd, _ := syscall.Socket(AF_BTH, syscall.SOCK_STREAM, BTHPROTO_RFCOMM)
```

## Protocol Design

### Wire Format

All messages use a compact binary format (little-endian):

```
┌──────────┬──────────┬─────────┬─────────────────┐
│ Type (1) │ Flags(1) │ Len (4) │ Payload (0..N)  │
└──────────┴──────────┴─────────┴─────────────────┘
```

- **Type** (uint8): Message type.
- **Flags** (uint8): Message flags (e.g., compression, last chunk).
- **Len** (uint32): Payload length in bytes.
- **Payload**: Variable-length, type-dependent.

Total header overhead: **6 bytes** — minimal for Bluetooth's constrained bandwidth.

### Message Types

```go
const (
    // Request types (client -> server)
    MsgListDir     uint8 = 0x01
    MsgGetInfo     uint8 = 0x02
    MsgDownload    uint8 = 0x03
    MsgUpload      uint8 = 0x04
    MsgDelete      uint8 = 0x05
    MsgMkdir       uint8 = 0x06
    MsgCopy        uint8 = 0x07
    MsgMove        uint8 = 0x08
    MsgChDir       uint8 = 0x09
    MsgPwd         uint8 = 0x0A

    // Response types (server -> client)
    MsgOK          uint8 = 0x80
    MsgError       uint8 = 0x81
    MsgDirListing  uint8 = 0x82
    MsgFileInfo    uint8 = 0x83
    MsgDataChunk   uint8 = 0x84
    MsgTransferEnd uint8 = 0x85

    // Flags
    FlagLastChunk  uint8 = 0x01
    FlagCompressed uint8 = 0x02
)
```

### Message Payloads

**ListDir Request**: `path (string, null-terminated)`

**DirListing Response**:
```
entry_count (uint32)
[repeated]:
  type     (uint8)  - 0=file, 1=dir, 2=symlink
  size     (uint64)
  mod_time (int64)  - unix timestamp
  mode     (uint32) - file permissions
  name_len (uint16)
  name     ([]byte)
```

**Download Request**: `path (string, null-terminated)`

**Upload Request**:
```
path_len   (uint16)
path       ([]byte)
total_size (uint64)
chunk_size (uint32)
```

**DataChunk**:
```
offset  (uint64)
crc32   (uint32)
data    ([]byte, length = Len - 12)
```

**TransferEnd**:
```
total_crc32 (uint32) - CRC32 of entire file
```

## Transfer Engine

### Download Flow

```
Client                          Server
  │                               │
  ├─── MsgDownload(path) ───────>│
  │                               ├── Open file
  │<── MsgFileInfo(size,name) ───┤
  │                               │
  │<── MsgDataChunk(off=0) ──────┤
  │<── MsgDataChunk(off=32K) ────┤
  │<── MsgDataChunk(off=64K) ────┤
  │     ...                       │
  │<── MsgTransferEnd(crc32) ────┤
  │                               │
  ├─── MsgOK ───────────────────>│  (ack)
```

### Upload Flow

```
Client                          Server
  │                               │
  ├─── MsgUpload(path,size) ────>│
  │<── MsgOK ────────────────────┤  (ready)
  │                               │
  ├─── MsgDataChunk(off=0) ─────>│
  ├─── MsgDataChunk(off=32K) ───>│
  ├─── MsgDataChunk(off=64K) ───>│
  │     ...                       │
  ├─── MsgTransferEnd(crc32) ───>│
  │                               │
  │<── MsgOK ────────────────────┤  (verified)
```

### Directory Transfer

For directory download/upload, the protocol walks the tree:

1. Client sends `MsgDownload` with directory path.
2. Server responds with `MsgDirListing` (recursive list with all files/subdirs).
3. For each file, server streams `MsgDataChunk` messages prefixed with relative path.
4. Client reconstructs directory structure locally.

Encoding for multi-file transfer:
```
MsgFileInfo (file 1 metadata)
MsgDataChunk (file 1 chunks...)
MsgTransferEnd (file 1)
MsgFileInfo (file 2 metadata)
MsgDataChunk (file 2 chunks...)
MsgTransferEnd (file 2)
...
MsgOK (all done)
```

### Performance Optimization

1. **Small chunk size (1 KB)**: Critical for RFCOMM flow control. The CSR8510 has ACL MTU=310 bytes with only 10 buffer slots. Writes >1KB cause RFCOMM credit exhaustion and severe stalls (throughput drops from 16 KB/s to <1 KB/s). Each message = 6-byte header + 12-byte chunk header + 1024 data = 1042 bytes total.

2. **Single-write message framing**: Header + payload combined into one `write()` syscall to avoid Nagle-style delays on RFCOMM stream sockets.

3. **Direct socket I/O**: No application-level buffering (`bufio.Writer`). The kernel BT socket has its own 64KB buffer (`SO_SNDBUF`/`SO_RCVBUF`). Double-buffering causes unpredictable flush stalls.

4. **Pipeline chunks**: Stream continuously without per-chunk ACK. CRC32 per chunk for error detection, full-file CRC32 for integrity verification at end.

5. **Hardware-accelerated CRC32**: Use `hash/crc32` with IEEE table (SSE4.2 on x86_64). Computed incrementally — no extra pass.

6. **Socket buffer tuning**: `SO_SNDBUF` and `SO_RCVBUF` = 65536 bytes on both ends.

7. **Link policy**: Disable SNIFF/PARK modes to prevent adapter sleep between transfers. Enable EDR packet types (2-DH5, 3-DH5).

### Measured Throughput (CSR8510 A10 x2, same host)

```
Test setup: 2x CSR8510 A10 USB dongles, BlueZ 5.64, Linux 6.8
  hci0 (server): 00:1A:7D:DA:71:11
  hci1 (client): 00:1A:7D:DA:71:22
  ACL MTU: 310 bytes, 10 buffer slots
  Packet types: DM1-DH5, 2-DH1/3/5, 3-DH1/3/5 (EDR enabled)
  Link quality: 255 (max), RSSI: +7

Raw RFCOMM throughput (no protocol):
  WriteSize=512:   16.2 KB/s (0.13 Mbps) — stable
  WriteSize=1024:  16.2 KB/s (0.13 Mbps) — stable
  WriteSize=4096:   0.7 KB/s (0.01 Mbps) — severe stalls

BFT application throughput (chunk=1024):
  Download 100 KB:  15.2 KB/s in  6.6s — stable, no stalls
  Upload   100 KB:  15.6 KB/s in  6.4s — stable, no stalls
  Download   1 MB:  15.4 KB/s in 66.4s — stable, no stalls

Protocol overhead: ~1.8% (18 bytes header per 1024 data)
App vs raw throughput: 95% efficiency (15.4/16.2 KB/s)

Note: The ~16 KB/s limit is a hardware/firmware characteristic of
CSR8510 clone adapters using DM1 baseband packets (~108 kbps).
Genuine BT 4.0 EDR adapters with 3-DH5 achieve 150-250 KB/s.
```

## Path Security

All server-side path operations go through `fsutil.SanitizePath()`:

```go
func SanitizePath(rootDir, requestedPath string) (string, error) {
    // 1. Clean the path (remove .., double slashes, etc.)
    cleaned := filepath.Clean(requestedPath)

    // 2. If relative, join with root
    if !filepath.IsAbs(cleaned) {
        cleaned = filepath.Join(rootDir, cleaned)
    }

    // 3. Resolve symlinks
    resolved, err := filepath.EvalSymlinks(cleaned)

    // 4. Verify resolved path is within root
    if !strings.HasPrefix(resolved, rootDir) {
        return "", ErrPathTraversal
    }

    return resolved, nil
}
```

## Error Handling

Error responses use `MsgError` with:
```
code    (uint16) - error code
message (string, null-terminated)
```

Error codes:
| Code | Meaning |
|------|---------|
| 1 | File not found |
| 2 | Permission denied |
| 3 | Path traversal attempt |
| 4 | Disk full |
| 5 | Transfer interrupted |
| 6 | Checksum mismatch |
| 7 | Invalid request |
| 8 | Server busy |

## Testing Strategy

### Unit Tests

| Component | What to Test |
|-----------|-------------|
| `protocol/message` | Encode/decode all message types, boundary values, malformed input |
| `protocol/handler` | Each request type with mocked filesystem and connection |
| `transfer/engine` | Chunk splitting, reassembly, CRC32 verification, resume logic |
| `transfer/checksum` | CRC32 correctness, incremental computation |
| `fsutil/pathutil` | Path traversal prevention, edge cases (symlinks, `..`, absolute paths) |
| `fsutil/fileops` | List, copy, move, delete with temp directories |
| `client/client` | Command parsing, response handling with mocked connection |
| `server/server` | Request routing, connection lifecycle with mocked transport |

### Integration Tests

- Use hci0 (server) and hci1 (client) on the same host.
- Prerequisite: change BD address of hci1 using `bdaddr` tool (both dongles ship with same address).
- Test full transfer lifecycle: connect, upload, download, verify content.
- Benchmark: transfer 1 MB, 10 MB, 100 MB files and measure throughput.

### Mock Transport

```go
// MockConn implements bt.Conn using in-memory pipe for unit testing.
type MockConn struct {
    reader *io.PipeReader
    writer *io.PipeWriter
}
```

This allows testing all protocol and transfer logic without actual Bluetooth hardware.

## Build

```makefile
# Linux (static, no CGO)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bft-linux-amd64 ./cmd/bft/

# Windows (static, no CGO)
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bft-windows-amd64.exe ./cmd/bft/
```
