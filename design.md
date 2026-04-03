# Blue File Transfer - Design

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                  CLI / Web GUI Layer                     │
│  (commands: server, client, scan, web)                  │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                   Protocol Layer                        │
│  (message encoding/decoding, MTU negotiation)           │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                  Transfer Engine                        │
│  (pipeline I/O, streaming compress, adaptive chunk,     │
│   CRC32, progress tracking)                             │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│         Bluetooth Transport Layer (+ encryption)        │
│  (platform-abstracted RFCOMM/L2CAP, AES-256-GCM,       │
│   ACL MTU discovery, dynamic socket buffers)            │
└──────────────────────┬──────────────────────────────────┘
                       │
         ┌─────────────┴─────────────┐
         │                           │
┌────────▼────────┐       ┌──────────▼────────┐
│  bt_linux.go    │       │  bt_windows.go    │
│  RFCOMM+L2CAP   │       │  RFCOMM           │
│  HCI ACL info   │       │  Winsock2 raw     │
│  dynamic sockbuf│       │  AF_BTH (0x20)    │
└─────────────────┘       └───────────────────┘
```

## Project Structure

```
blue-file-transfer/
├── cmd/
│   └── bft/
│       ├── main.go              # Entry point, flag parsing
│       └── benchmark.go         # Transfer speed benchmarks
├── internal/
│   ├── bt/
│   │   ├── transport.go         # Transport/Conn/Listener interfaces
│   │   ├── bt_linux.go          # Linux RFCOMM, ACL MTU discovery, dynamic sockbuf
│   │   ├── l2cap_linux.go       # Linux L2CAP transport (higher throughput)
│   │   ├── bt_windows.go        # Windows RFCOMM (Winsock2)
│   │   ├── factory_linux.go     # Transport factory (RFCOMM/L2CAP)
│   │   ├── factory_windows.go   # Transport factory (RFCOMM only)
│   │   └── mock.go              # Mock transport for testing (io.Pipe)
│   ├── protocol/
│   │   ├── message.go           # Message types, MTU negotiation, encoding
│   │   └── message_test.go      # Encode/decode tests
│   ├── transfer/
│   │   ├── engine.go            # Core send/receive with drain-on-error recovery
│   │   ├── pipeline.go          # Pipeline I/O, adaptive chunking, streaming compress
│   │   ├── compress.go          # DEFLATE compression
│   │   ├── checksum.go          # CRC32 (hardware-accelerated)
│   │   ├── engine_test.go       # Core transfer tests
│   │   ├── pipeline_test.go     # Pipeline, adaptive chunk, streaming compress tests
│   │   ├── compress_test.go     # Compression tests
│   │   └── checksum_test.go     # CRC32 tests
│   ├── server/
│   │   ├── server.go            # Concurrent connections, MTU negotiation, max-clients
│   │   └── server_test.go       # Server tests
│   ├── client/
│   │   ├── client.go            # MTU negotiation, pipeline upload
│   │   ├── client_test.go       # Client tests
│   │   └── cli.go               # Interactive CLI
│   ├── crypto/
│   │   └── stream.go            # AES-256-GCM encryption with HKDF
│   ├── auth/
│   │   └── auth.go              # User authentication (salted SHA-256)
│   ├── web/
│   │   ├── handler.go           # Web API (connect/disconnect/status endpoints)
│   │   └── html.go              # Embedded web GUI with connection status bar
│   └── fsutil/
│       ├── pathutil.go          # Path sanitization, traversal prevention
│       └── fileops.go           # File operations (ls, cp, mv, rm, mkdir)
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
// Dynamic socket buffer based on ACL capacity: 2 * aclMTU * aclPkts
// Clamped to [8KB, 256KB], defaults to 64KB if ACL info unavailable
bufSize := dynamicSockBuf(adapter)
unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, bufSize)
unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, bufSize)
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
    MsgExec        uint8 = 0x0B  // Remote command execution
    MsgAuth        uint8 = 0x0C  // Authentication (username + password)
    MsgPasswd      uint8 = 0x0D  // Change password
    MsgShell       uint8 = 0x0E  // Interactive shell session
    MsgShellIn     uint8 = 0x0F  // Shell stdin
    MsgMTU         uint8 = 0x10  // MTU negotiation (bidirectional)

    // Response types (server -> client)
    MsgOK          uint8 = 0x80
    MsgError       uint8 = 0x81
    MsgDirListing  uint8 = 0x82
    MsgFileInfo    uint8 = 0x83
    MsgDataChunk   uint8 = 0x84
    MsgTransferEnd uint8 = 0x85
    MsgExecOutput  uint8 = 0x86  // Streaming command output
    MsgExecExit    uint8 = 0x87  // Command exit code

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

### Connection Handshake (MTU + Auth + Encryption)

```
Client                              Server
  |                                   |
  |-- MsgMTU(acl_mtu, acl_pkts, ───>|  (optional, first message)
  |          chunk_size)              |
  |<── MsgMTU(acl_mtu, acl_pkts, ──|  agreed = min(client, server)
  |          chunk_size)              |
  |                                   |
  |-- MsgAuth(user, pass) ────────>|  (if auth configured)
  |<── MsgOK ──────────────────────|
  |                                   |
  |<── server_nonce (32 bytes) ────|  (encryption key exchange)
  |── client_nonce (32 bytes) ────>|
  |                                   |
  |═══ AES-256-GCM encrypted ═════|  (all subsequent messages)
```

**MTU Payload** (8 bytes): `acl_mtu(2) + acl_pkts(2) + chunk_size(4)`

MTU negotiation is optional and backward-compatible: if the first message is not MsgMTU (e.g., MsgAuth or a command), the server skips negotiation and uses the default chunk size (1024 bytes).

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

1. **ACL MTU discovery and adaptive chunk size**: At connection time, both sides read the adapter's ACL MTU and buffer slot count via `HCIGETDEVINFO` ioctl. Chunk size = `acl_mtu * acl_pkts / 2`, clamped to [1KB, 64KB]. An `AdaptiveChunker` further adjusts at runtime: doubles size on fast writes (<5ms), halves on slow writes (>50ms, indicating flow control stalls).

2. **Pipeline I/O**: File transfers use separate reader and writer goroutines connected by a buffered channel (depth=4). The reader prepares chunks (read + CRC + compress + encode) while the writer sends pre-encoded messages. This overlaps disk I/O with Bluetooth socket I/O, providing ~30-50% speedup on real BT links.

3. **Streaming compression**: A `StreamCompressor` reuses the `flate.Writer` and `bytes.Buffer` across chunks instead of allocating new ones per chunk. This is **53x faster** than per-chunk compression (281 MB/s vs 5.3 MB/s) with **20x fewer allocations**.

4. **Single-write message framing**: Header + payload combined into one `write()` syscall to avoid Nagle-style delays on RFCOMM stream sockets.

5. **Dynamic socket buffers**: `SO_SNDBUF` and `SO_RCVBUF` set to `2 * aclMTU * aclPkts`, clamped to [8KB, 256KB]. Adapters with more buffer capacity get larger socket buffers automatically.

6. **Direct socket I/O**: No application-level buffering (`bufio.Writer`). The kernel BT socket has its own tuned buffer. Double-buffering causes unpredictable flush stalls.

7. **Hardware-accelerated CRC32**: Use `hash/crc32` with IEEE table (SSE4.2 on x86_64, PMULL on ARM64). Computed incrementally — no extra pass. `CRC32Writer` allows computing CRC on-the-fly during write operations.

8. **Transfer recovery**: On mid-transfer errors (CRC mismatch, disk full), remaining protocol messages are drained to keep the stream synchronized. The session remains usable for subsequent commands without reconnecting. Partial files are automatically cleaned up.

9. **Concurrent server**: Multiple client connections handled via goroutines with `--max-clients` limit. When exceeded, the oldest connection is evicted. Bluetooth adapter is auto-configured (up + piscan) before Listen/Connect.

10. **Link policy**: Disable SNIFF/PARK modes to prevent adapter sleep between transfers. Enable EDR packet types (2-DH5, 3-DH5).

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
| 9 | Remote exec disabled |
| 10 | Authentication required |
| 11 | Authentication failed |

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
