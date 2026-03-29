# Blue File Transfer - Implementation Plan

## Pre-requisites

### P1: Fix Bluetooth Adapter Addresses
Both CSR8510 A10 dongles have the same BD Address (`00:1A:7D:DA:71:11`). Before any integration testing:
```bash
# Install bdaddr tool (part of bluez-utils or build from bluez source)
sudo apt install bluez

# Bring hci1 up and change its address
sudo hciconfig hci1 up
sudo bdaddr -i hci1 00:1A:7D:DA:71:22
sudo hciconfig hci1 reset
sudo hciconfig hci1 up

# Verify different addresses
hciconfig -a
```

### P2: Initialize Go Module
```bash
cd ~/Documents/blue-file-transfer
go mod init blue-file-transfer
```

---

## Phase 1: Foundation (Core Infrastructure)

### Step 1.1: Project Scaffold
- [ ] Create directory structure (`cmd/bft/`, `internal/bt/`, `internal/protocol/`, `internal/transfer/`, `internal/server/`, `internal/client/`, `internal/fsutil/`)
- [ ] Initialize `go.mod`
- [ ] Create `cmd/bft/main.go` with cobra CLI skeleton (server, client, scan subcommands)
- [ ] Create `Makefile` with build targets for Linux and Windows

### Step 1.2: Bluetooth Transport - Linux
- [ ] Define `Transport`, `Listener`, `Conn`, `Device` interfaces in `internal/bt/transport.go`
- [ ] Implement `bt_linux.go`: RFCOMM socket Listen, Accept, Connect, Close
- [ ] Implement adapter selection by HCI device name (resolve hciN -> BD address)
- [ ] Set socket buffer sizes (SO_SNDBUF, SO_RCVBUF = 65536)
- [ ] Write unit test: create listener, verify it binds without error
- [ ] Write unit test: mock transport for use by other packages

### Step 1.3: Bluetooth Transport - Windows (Stub)
- [ ] Implement `bt_windows.go` with AF_BTH raw syscall structures
- [ ] Define `SOCKADDR_BTH` struct and marshaling
- [ ] Stub methods returning `ErrNotImplemented` for initial testing on Linux
- [ ] Mark with `// TODO: full implementation` for later Windows testing

### Step 1.4: Device Discovery
- [ ] Implement `discovery_linux.go`: HCI inquiry scan via raw HCI socket
- [ ] Parse inquiry results to extract BD address and device name
- [ ] Implement `discovery_windows.go`: stub
- [ ] Write test for discovery result parsing (mock HCI response bytes)

**Tests for Phase 1:**
- `bt/transport_test.go` — interface compliance, mock transport
- `bt/bt_linux_test.go` — socket creation, bind (requires BT adapter)
- `bt/discovery_linux_test.go` — inquiry result parsing

---

## Phase 2: Protocol Layer

### Step 2.1: Message Encoding/Decoding
- [ ] Define message header struct (Type, Flags, Len)
- [ ] Implement `WriteMessage(w io.Writer, msg Message) error`
- [ ] Implement `ReadMessage(r io.Reader) (Message, error)`
- [ ] Define payload types: ListDirReq, FileInfo, DirListing, DataChunk, TransferEnd, ErrorResp
- [ ] Implement encode/decode for each payload type

**Tests:**
- [ ] Round-trip encode/decode for every message type
- [ ] Boundary tests: empty payload, max payload size, zero-length strings
- [ ] Fuzz test: random bytes should not panic decoder

### Step 2.2: Server Request Handler
- [ ] Implement `handler.go`: dispatch by message type
- [ ] Handle MsgListDir: read directory, return MsgDirListing
- [ ] Handle MsgGetInfo: stat file, return MsgFileInfo
- [ ] Handle MsgDelete: remove file/dir
- [ ] Handle MsgMkdir: create directory
- [ ] Handle MsgCopy: copy file/dir (server-side)
- [ ] Handle MsgMove: rename/move
- [ ] Handle MsgChDir: change working directory
- [ ] Handle MsgPwd: return current directory

**Tests:**
- [ ] Each handler with temp directory filesystem
- [ ] Error cases: nonexistent paths, permission denied simulation
- [ ] Path traversal attempts must be rejected

---

## Phase 3: File System Utilities

### Step 3.1: Path Sanitization
- [ ] Implement `SanitizePath(rootDir, requestedPath)` in `fsutil/pathutil.go`
- [ ] Handle: relative paths, `..`, absolute paths, symlinks, double slashes
- [ ] Return error on path traversal attempt

**Tests (critical for security):**
- [ ] `../../etc/passwd` -> error
- [ ] `./subdir/../../../root` -> error
- [ ] Symlink pointing outside root -> error
- [ ] Normal relative path -> success
- [ ] Absolute path within root -> success
- [ ] Deeply nested valid path -> success

### Step 3.2: File Operations
- [ ] `ListDir(path)` -> []FileEntry
- [ ] `CopyFileOrDir(src, dst)` — recursive for directories
- [ ] `MoveFileOrDir(src, dst)`
- [ ] `RemoveFileOrDir(path)` — recursive for directories
- [ ] `MkdirAll(path)`
- [ ] `FileInfo(path)` -> metadata

**Tests:**
- [ ] All operations with temp directories
- [ ] Copy preserves content and structure
- [ ] Remove is recursive
- [ ] Operations on nonexistent paths return proper errors

---

## Phase 4: Transfer Engine

### Step 4.1: Chunked Transfer Core
- [ ] Implement `SendFile(conn, filePath, chunkSize)` — reads file, sends DataChunk messages
- [ ] Implement `ReceiveFile(conn, destPath)` — receives DataChunk messages, writes to file
- [ ] Incremental CRC32 computation during send/receive
- [ ] Final CRC32 verification via TransferEnd message

**Tests:**
- [ ] Transfer 0-byte file
- [ ] Transfer small file (< 1 chunk)
- [ ] Transfer large file (multiple chunks)
- [ ] CRC32 mismatch detection
- [ ] Interrupted transfer handling

### Step 4.2: Directory Transfer
- [ ] Implement `SendDir(conn, dirPath, chunkSize)` — walks tree, sends FileInfo + chunks per file
- [ ] Implement `ReceiveDir(conn, destPath)` — recreates directory structure
- [ ] Preserve relative paths within transferred directory

**Tests:**
- [ ] Transfer empty directory
- [ ] Transfer directory with nested subdirectories
- [ ] Transfer directory with mixed files and subdirs
- [ ] Verify structure and content match

### Step 4.3: Progress Tracking
- [ ] Implement progress callback: bytes transferred, total, percentage, speed (bytes/sec)
- [ ] CLI progress bar rendering

### Step 4.4: Resume Support
- [ ] On download: check if partial file exists, send offset in request
- [ ] On upload: server reports existing size, client skips to offset
- [ ] Verify CRC32 of existing portion before resuming

**Tests:**
- [ ] Simulate interrupted transfer, resume, verify final file
- [ ] Resume with corrupted partial file -> restart from beginning

---

## Phase 5: Server & Client Assembly

### Step 5.1: Server
- [ ] `server.go`: bind Bluetooth listener, accept connection, run handler loop
- [ ] Manage server state: root directory, current directory per session
- [ ] Graceful shutdown on SIGINT/SIGTERM
- [ ] Logging: connection events, operations, errors

### Step 5.2: Client
- [ ] `client.go`: connect to server, send requests, handle responses
- [ ] `cli.go`: interactive readline loop with command parsing
- [ ] Command dispatch: parse command + args -> protocol message -> send -> display response
- [ ] Tab completion for remote paths (optional, stretch goal)

**Tests:**
- [ ] Client-server round-trip using MockConn (in-memory pipe)
- [ ] Each CLI command produces correct protocol message
- [ ] Server handles rapid sequential commands

---

## Phase 6: CLI & Build

### Step 6.1: CLI Polish
- [ ] `main.go`: wire up cobra commands (server, client, scan)
- [ ] Flag parsing: adapter, directory, channel, timeout
- [ ] Help text and usage examples
- [ ] Version flag

### Step 6.2: Makefile
- [ ] `make build` — build for current platform
- [ ] `make linux` — cross-compile Linux amd64
- [ ] `make windows` — cross-compile Windows amd64
- [ ] `make test` — run all unit tests
- [ ] `make test-integration` — run integration tests (requires 2 BT adapters)
- [ ] `make bench` — run benchmark tests
- [ ] `make coverage` — generate coverage report
- [ ] `make clean`

---

## Phase 7: Integration Testing & Optimization

### Step 7.1: Two-Adapter Test Setup
- [ ] Script to configure hci0 and hci1 with different BD addresses
- [ ] Script to start server on hci0, client on hci1
- [ ] Integration test: scan, connect, list, upload, download, verify
- [ ] Integration test: upload large file, measure throughput
- [ ] Integration test: directory transfer

### Step 7.2: Throughput Benchmarks
- [ ] Benchmark: 1 MB file transfer (both directions)
- [ ] Benchmark: 10 MB file transfer
- [ ] Benchmark: 100 MB file transfer
- [ ] Benchmark: vary chunk size (4KB, 8KB, 16KB, 32KB, 64KB) and measure effect
- [ ] Profile with `go tool pprof` — identify bottlenecks
- [ ] Tune socket buffer sizes based on results

### Step 7.3: Stress Testing
- [ ] Rapid connect/disconnect cycles
- [ ] Transfer with simulated Bluetooth interference (move dongles apart)
- [ ] Many small files (1000x 1KB files)
- [ ] Single very large file (1 GB)

---

## Phase 8: Windows Implementation

### Step 8.1: Windows Bluetooth Transport
- [ ] Implement full `bt_windows.go`: Connect, Listen, Accept via Winsock AF_BTH
- [ ] Implement `discovery_windows.go`: WSALookupService for device scan
- [ ] Test on Windows x64 with CSR8510 dongle
- [ ] Verify static binary (no DLL dependencies beyond system)

### Step 8.2: Windows-Specific Testing
- [ ] Cross-platform transfer: Linux server <-> Windows client
- [ ] Cross-platform transfer: Windows server <-> Linux client
- [ ] Verify path handling with backslash/forwardslash

---

## Summary: Test Requirements

| Category | Count | Coverage Target |
|----------|-------|-----------------|
| Protocol message encode/decode | ~20 tests | 100% |
| Path sanitization/security | ~10 tests | 100% |
| File operations | ~12 tests | 90% |
| Transfer engine | ~12 tests | 90% |
| Server handler | ~15 tests | 85% |
| Client commands | ~12 tests | 85% |
| Integration (BT) | ~8 tests | — |
| Benchmarks | ~6 tests | — |
| **Total** | **~95 tests** | **80%+ overall** |

## Implementation Order

```
Phase 1 (Foundation)     ████████░░  ~2 days
Phase 2 (Protocol)       ████████░░  ~2 days
Phase 3 (FS Utilities)   ██████░░░░  ~1 day
Phase 4 (Transfer)       ████████░░  ~2 days
Phase 5 (Server/Client)  ████████░░  ~2 days
Phase 6 (CLI/Build)      ████░░░░░░  ~1 day
Phase 7 (Integration)    ██████░░░░  ~1-2 days
Phase 8 (Windows)        ██████░░░░  ~1-2 days
```

Total estimated phases: 8 phases, incremental and testable at each step.
