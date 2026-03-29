# Blue File Transfer - Requirements

## Overview

Bluetooth-based file transfer application with client-server architecture. Allows a client to browse, upload, download, copy, and delete files/directories on a remote server over Bluetooth Classic.

## Functional Requirements

### FR-01: Server Mode
- The application runs as a server, listening for incoming Bluetooth connections.
- Server exposes a specified root directory for client operations.
- Server must support only one active client connection at a time (Bluetooth point-to-point).
- Server advertises its service via Bluetooth SDP (Service Discovery Protocol).

### FR-02: Client Mode
- The application connects to a remote server via Bluetooth address.
- Client provides a CLI interface for all file operations.
- Client supports device discovery to find available servers.

### FR-03: File Operations (Client -> Server)
| Operation | Description |
|-----------|-------------|
| `ls <path>` | List files and directories at path on server |
| `cd <path>` | Change current remote working directory |
| `pwd` | Print current remote working directory |
| `info <path>` | Get file/directory metadata (size, permissions, modified time) |
| `download <remote> [local]` | Download file or directory from server to local machine |
| `upload <local> [remote]` | Upload file or directory from local to server |
| `rm <path>` | Delete file or directory on server |
| `mkdir <path>` | Create directory on server |
| `cp <src> <dst>` | Copy file or directory on server (server-side operation) |
| `mv <src> <dst>` | Move/rename file or directory on server |

### FR-04: Directory Transfer
- Recursive download/upload of directories preserving structure.
- Preserve file permissions where possible (Linux).

### FR-05: Transfer Optimization
- Chunked transfer with configurable chunk size (default 32 KB).
- Progress indication during transfer (bytes transferred, percentage, speed).
- CRC32 checksum verification per chunk for data integrity.
- Resume support for interrupted transfers.

### FR-06: Device Discovery
- `scan` command to discover nearby Bluetooth devices.
- `connect <address>` to connect to a specific server.
- `disconnect` to close connection.

### FR-07: Security
- Server restricts operations to within the specified root directory (no path traversal).
- Optional PIN-based Bluetooth pairing.

## Non-Functional Requirements

### NFR-01: Platform Support
- Linux x86_64 (primary target).
- Windows x86_64 (secondary target).
- Single static binary per platform (no external dependencies at runtime).

### NFR-02: Performance
- Maximize Bluetooth throughput — target near theoretical max for CSR8510 A10 (~2.1 Mbps EDR, ~270 KB/s practical).
- Large write buffers (32-64 KB application level) to keep the Bluetooth pipe saturated.
- Minimize protocol overhead: compact binary message format.
- Concurrent chunk checksumming (don't block I/O for CRC computation).

### NFR-03: Build
- Written in Go (Golang).
- `CGO_ENABLED=0` on Linux (pure Go using `golang.org/x/sys/unix` for Bluetooth sockets).
- Windows: pure Go via `syscall` with manually defined `AF_BTH` structures (avoid CGO if possible).
- Cross-compilation support via Makefile/build script.

### NFR-04: Testing
- Unit tests for all protocol message encoding/decoding.
- Unit tests for path validation and sanitization (security).
- Unit tests for chunked transfer logic (split, reassemble, checksum).
- Integration tests using two Bluetooth adapters on the same host (hci0 as server, hci1 as client).
- Mock-based tests for platform-abstracted Bluetooth layer.
- Minimum 80% code coverage for non-platform-specific code.
- Benchmark tests for transfer throughput measurement.

### NFR-05: Reliability
- Graceful handling of Bluetooth disconnections mid-transfer.
- Timeout on all operations (configurable, default 30s for commands, 5min for transfers).
- Clean error messages for all failure modes.

## Constraints

### Hardware
- Testing with 2x CSR8510 A10 USB dongles (Cambridge Silicon Radio, Bluetooth 4.0).
- Known issue: both dongles may share the same BD Address — must use `bdaddr` tool to change one adapter's address before testing.
- ACL MTU: 310 bytes (hardware limit of CSR8510).
- HCI Version: 4.0.

### Software
- Go 1.25.6+.
- Linux: BlueZ 5.64+ kernel stack.
- Windows: Windows Bluetooth Winsock API (built-in).

## CLI Interface

```
blue-file-transfer server [flags]
  --adapter <hci>     Bluetooth adapter (default: hci0)
  --dir <path>        Root directory to serve (default: current dir)
  --channel <n>       RFCOMM channel (default: 1)

blue-file-transfer client [flags]
  --adapter <hci>     Bluetooth adapter (default: hci0)

blue-file-transfer scan [flags]
  --adapter <hci>     Bluetooth adapter (default: hci0)
  --timeout <sec>     Scan timeout (default: 10)
```

### Client Interactive Commands
```
connect <bt-address>          Connect to server
disconnect                    Disconnect from server
scan                          Scan for devices
ls [path]                     List remote directory
cd <path>                     Change remote directory
pwd                           Print remote working directory
info <path>                   File/directory info
download <remote> [local]     Download file/directory
upload <local> [remote]       Upload file/directory
rm <path>                     Remove file/directory
mkdir <path>                  Create directory
cp <src> <dst>                Copy on server
mv <src> <dst>                Move/rename on server
exit                          Exit client
```
