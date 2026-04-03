# Blue File Transfer (BFT) - Usage Guide

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation - Linux](#installation---linux)
- [Installation - Windows](#installation---windows)
- [Building from Source](#building-from-source)
- [Hardware Setup](#hardware-setup)
- [Authentication & Encryption](#authentication--encryption)
- [Usage](#usage)
  - [One-shot Commands](#one-shot-commands)
  - [Interactive Client](#interactive-client)
- [Benchmark](#benchmark)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

### Hardware

- 1x or 2x Bluetooth USB adapters (tested with **CSR8510 A10**)
- Bluetooth 2.1+ EDR recommended for best throughput
- Both devices must support **RFCOMM** (Bluetooth Classic, not BLE)

### Software

| Component | Linux | Windows |
|-----------|-------|---------|
| OS | Ubuntu 20.04+ / Debian 11+ | Windows 10/11 x64 |
| Bluetooth stack | BlueZ 5.50+ | Built-in Windows BT stack |
| Go (build only) | 1.21+ | 1.21+ |

---

## Installation - Linux

### 1. Install Bluetooth stack (BlueZ)

```bash
# Ubuntu / Debian
sudo apt update
sudo apt install -y bluez bluez-tools bluetooth

# Fedora / RHEL
sudo dnf install -y bluez bluez-tools

# Arch Linux
sudo pacman -S bluez bluez-utils
```

### 2. Install development headers (build only)

```bash
# Ubuntu / Debian
sudo apt install -y libbluetooth-dev

# Fedora / RHEL
sudo dnf install -y bluez-libs-devel

# Arch Linux
sudo pacman -S bluez-libs
```

> **Note**: `libbluetooth-dev` is NOT required at runtime. The BFT binary is fully static and uses raw syscalls. It is only needed if you want to use `hcitool`, `sdptool`, etc.

### 3. Enable and start Bluetooth service

```bash
sudo systemctl enable bluetooth
sudo systemctl start bluetooth

# Verify service is running
systemctl status bluetooth
```

### 4. Verify Bluetooth adapters

```bash
# List adapters
hciconfig -a

# Expected output:
# hci0: Type: Primary  Bus: USB
#       BD Address: 00:1A:7D:DA:71:11  ACL MTU: 310:10
#       UP RUNNING

# If adapter is DOWN, bring it up:
sudo hciconfig hci0 up
```

### 5. Install BFT binary

Download the pre-built binary or [build from source](#building-from-source):

```bash
# Copy binary to system path
sudo cp bft-linux-amd64 /usr/local/bin/bft
sudo chmod +x /usr/local/bin/bft

# Verify
bft version
```

### 6. Permissions (optional, avoid running as root)

BFT needs access to Bluetooth sockets. Either run as root or add capabilities:

```bash
# Option A: Run with sudo
sudo bft server --dir /shared

# Option B: Grant Bluetooth capabilities (recommended)
sudo setcap 'cap_net_admin,cap_net_raw+eip' /usr/local/bin/bft

# Then run without sudo:
bft server --dir /shared
```

---

## Installation - Windows

### 1. Bluetooth driver

Windows 10/11 includes a built-in Bluetooth driver. Ensure:

- Bluetooth adapter is detected in **Device Manager** > **Bluetooth**
- The adapter shows "CSR8510 A10" or similar under **Bluetooth Radios**
- If driver is missing, download from [Cambridge Silicon Radio](https://www.qualcomm.com/products/technology/bluetooth) or use Windows Update

### 2. Enable Bluetooth

```
Settings > Devices > Bluetooth & other devices > Turn ON
```

### 3. Install BFT

```powershell
# Copy binary to a directory in PATH
copy bft-windows-amd64.exe C:\Tools\bft.exe

# Verify
bft.exe version
```

> **Note**: Windows Firewall may prompt to allow BFT. Click "Allow access" for both private and public networks.

---

## Building from Source

### Install Go

```bash
# Linux
wget https://go.dev/dl/go1.25.6.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.25.6.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Verify
go version
```

### Build

```bash
git clone <repository-url>
cd blue-file-transfer

# Build for current platform
make build

# Build for Linux x64 (static binary, no CGO)
make linux

# Build for Windows x64 (cross-compile from Linux)
make windows

# Build both
make all
```

Output binaries:
- `bft-linux-amd64` (Linux, ~2.1 MB)
- `bft-windows-amd64.exe` (Windows, ~2.2 MB)

### Run tests

```bash
# All unit tests
make test

# With coverage report
make coverage
# Open coverage.html in browser

# Benchmarks
make bench
```

---

## Hardware Setup

### Single adapter (two separate machines)

Standard setup. One machine runs `bft server`, the other runs `bft client`.

### Two adapters on same machine (testing)

For development/testing with 2x CSR8510 on the same host:

#### Step 1: Verify both adapters are detected

```bash
hciconfig
# Should show hci0 and hci1
```

#### Step 2: Change BD Address of second adapter

Both CSR8510 clones often ship with the same BD Address. You MUST change one:

```bash
# Bring hci1 up
sudo hciconfig hci1 up

# Change BD Address via CSR vendor command
# Target: 00:1A:7D:DA:71:22 (change last byte)
sudo hcitool -i hci1 cmd 0x3f 0x00 \
  0xc2 \
  0x02 0x00 0x0c 0x00 0x11 0x47 0x03 0x70 \
  0x00 0x00 0x01 0x00 0x04 0x00 0x00 0x00 \
  0xDA 0x00 \
  0x22 0x71 \
  0x7D 0x00 \
  0x1A 0x00

# Send warm reset to apply
sudo hcitool -i hci1 cmd 0x3f 0x00 \
  0xc2 \
  0x02 0x00 0x09 0x00 0x00 0x00 0x02 0x40 \
  0x00 0x00 0x00 0x00 0x00 0x00 0x00 0x00 \
  0x00 0x00 0x00 0x00

# Wait for USB re-enumeration, then bring up
sleep 2
sudo hciconfig hci1 up

# Verify different addresses
hciconfig
# hci0: BD Address: 00:1A:7D:DA:71:11
# hci1: BD Address: 00:1A:7D:DA:71:22
```

> **Note**: This address change persists across reboots for CSR8510. To use a temporary (transient) change, set byte at position 15 to `0x08` instead of `0x00`.

#### Step 3: Optimize adapter settings

```bash
# Enable EDR packet types for better throughput
sudo hciconfig hci0 ptype DM1,DM3,DM5,DH1,DH3,DH5,2-DH1,2-DH3,2-DH5,3-DH1,3-DH3,3-DH5
sudo hciconfig hci1 ptype DM1,DM3,DM5,DH1,DH3,DH5,2-DH1,2-DH3,2-DH5,3-DH1,3-DH3,3-DH5

# Disable SNIFF/PARK to prevent sleep
sudo hciconfig hci0 lp RSWITCH
sudo hciconfig hci1 lp RSWITCH

# Make server adapter discoverable
sudo hciconfig hci0 piscan
```

---

## Authentication & Encryption

BFT supports multi-user authentication with AES-256-GCM encrypted data transfer.

### Setup Users

```bash
# Add users
bft useradd --users-file users.json --user admin --pass mypassword
bft useradd --users-file users.json --user guest --pass guest123

# List users
bft userlist --users-file users.json

# Remove a user
bft userdel --users-file users.json --user guest
```

The users file stores salted SHA-256 password hashes (file permissions: 0600):
```json
[
  {
    "username": "admin",
    "pass_hash": "26dfba01...",
    "salt": "dde87434..."
  }
]
```

### Server with Authentication

```bash
# Auth enabled — all connections require username/password
# All data encrypted with AES-256-GCM after login
sudo bft server --dir /shared --users-file users.json

# Without --users-file — no auth, no encryption (backward compatible)
sudo bft server --dir /shared
```

### Client Login

```bash
# Credentials via flags
sudo bft client --user admin --pass mypassword

# Or interactive prompt (password masked with *)
sudo bft client
bft> connect 00:1A:7D:DA:71:22
Username (empty=no auth): admin
Password: ********
Connected!
```

### Change Password

```bash
bft> passwd
Current password: ********
New password: ********
Confirm new password: ********
Password changed successfully.
```

### Encryption Details

When authentication is enabled, all data after login is encrypted end-to-end:

```
Client                              Server
  |                                   |
  |-- MsgAuth(user, pass) ---------> |  plaintext (one-time)
  |<-- MsgOK ------------------------|
  |                                   |
  |<-- server_nonce (32 bytes) ------|  key exchange
  |-- client_nonce (32 bytes) ------>|
  |                                   |
  |   key = HKDF-SHA256(password,     |
  |         server_nonce || client_nonce)
  |                                   |
  |== AES-256-GCM encrypted ========|  all subsequent data
  |-- [len][nonce][ciphertext+tag] ->|
  |<- [len][nonce][ciphertext+tag] --|
```

| Property | Value |
|----------|-------|
| Cipher | AES-256-GCM |
| Key derivation | HKDF-SHA256 |
| Key length | 256 bits |
| Nonce | 12 bytes (8-byte counter + 4 random) |
| Auth tag | 16 bytes per frame |
| Forward secrecy | Per-session (random nonces) |

### Hardware Acceleration

AES-256-GCM uses hardware crypto instructions on all supported platforms — no software fallback needed:

| Platform | CPU Feature | Instructions | Auto-detected |
|----------|-------------|-------------|---------------|
| x86_64 | AES-NI | `AESENC`/`AESDEC`, `PCLMULQDQ` | Yes |
| ARM64 (Jetson, RPi4+) | ARMv8 Crypto | `AESE`/`AESD`/`AESMC`, `PMULL` | Yes |
| ARM64 | SHA2 | `SHA256H` | Yes (HKDF) |
| x86_64 / ARM64 | CRC32 | `CRC32` hardware instruction | Yes (chunk integrity) |

Go's `crypto/aes` and `hash/crc32` packages detect these features at runtime. Encryption overhead is minimal (~10%: 1.34 Mbps encrypted vs 1.50 Mbps unencrypted on Bluetooth L2CAP).

---

## Usage

### One-shot Commands

Run a single command without entering the interactive shell. Connects, authenticates, encrypts, executes, and exits:

```bash
# List files on remote server
bft ls --server 00:1A:7D:DA:71:22 --user admin --pass secret

# Download a file
bft download --server 00:1A:7D:DA:71:22 --path document.pdf --local /tmp

# Upload a file
bft upload --server 00:1A:7D:DA:71:22 --path ./backup.tar.gz

# Execute remote command (exit code propagated to shell)
bft exec --server 00:1A:7D:DA:71:22 --cmd "df -h /"

# File operations
bft mkdir --server 00:1A:7D:DA:71:22 --path newdir
bft rm    --server 00:1A:7D:DA:71:22 --path oldfile.txt
bft cp    --server 00:1A:7D:DA:71:22 --src a.txt --dst b.txt
bft mv    --server 00:1A:7D:DA:71:22 --src old --dst new
bft info  --server 00:1A:7D:DA:71:22 --path file.bin
bft pwd   --server 00:1A:7D:DA:71:22
```

All one-shot commands support `--user`, `--pass`, `--channel`, `--rfcomm`, `--no-compress`, `--adapter`.

Use in scripts:
```bash
#!/bin/bash
SERVER="00:1A:7D:DA:71:22"
AUTH="--user admin --pass secret"

# Backup remote logs
bft download --server $SERVER $AUTH --path /var/log/syslog --local ./backups/
bft exec --server $SERVER $AUTH --cmd "logrotate -f /etc/logrotate.conf"
```

### Interactive Client

### Starting the Server

### Starting the Server

The server exposes a directory over Bluetooth for remote file operations.

```bash
bft server [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--adapter` | `hci0` | Bluetooth adapter to use |
| `--dir` | `.` (current dir) | Root directory to serve |
| `--channel` | `1` | RFCOMM channel 1-30 (or L2CAP PSM mapping) |
| `--rfcomm` | (default: L2CAP) | Use RFCOMM transport instead of L2CAP |
| `--users-file` | (none) | Users file for auth + encryption |
| `--no-exec` | (exec ON) | Disable remote command execution |
| `--max-clients` | unlimited | Max concurrent connections (oldest dropped when exceeded) |

**Examples:**

```bash
# Basic server (L2CAP, no auth)
sudo bft server --dir /home/user/shared

# Server with auth + encryption
sudo bft server --dir /home/user/shared --users-file users.json

# RFCOMM mode (cross-platform), specific channel
sudo bft server --dir /home/user/shared --rfcomm --channel 5

# Disable remote exec
sudo bft server --dir /home/user/shared --no-exec

# Limit concurrent connections (oldest dropped when exceeded)
sudo bft server --dir /home/user/shared --max-clients 3
```

The server will display:
```
Starting BFT server [rfcomm] on hci0 channel 1, serving: /home/user/shared
[server] Listening on 00:1A:7D:DA:71:11 channel 1
[server] Client connected: 00:1A:7D:DA:71:22
```

### Starting the Client

The client connects to a server and provides an interactive CLI.

```bash
bft client [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--adapter` | `hci0` | Bluetooth adapter to use |
| `--no-compress` | (compression ON) | Disable DEFLATE compression |
| `--rfcomm` | (default: L2CAP) | Use RFCOMM transport instead of L2CAP |
| `--user` | (none) | Username for authentication |
| `--pass` | (none) | Password for authentication |

**Examples:**

```bash
# Default: L2CAP + compression, no auth
sudo bft client --adapter hci1

# With authentication (enables AES-256-GCM encryption)
sudo bft client --user admin --pass mypassword

# RFCOMM mode
sudo bft client --rfcomm

# Interactive auth prompt on connect
sudo bft client
bft> connect 00:1A:7D:DA:71:22
Username (empty=no auth): admin
Password: ********
```

> **Important**: Both server and client must use the same transport (L2CAP default or `--rfcomm`). They cannot mix.

### Client Commands

Once in the interactive CLI:

#### Connection

```bash
# Scan for nearby Bluetooth devices
bft> scan

# Connect to a server
bft> connect 00:1A:7D:DA:71:11
# Or with specific channel:
bft> connect 00:1A:7D:DA:71:11 5

# Disconnect
bft> disconnect
```

#### Browsing

```bash
# List files in current remote directory
bft> ls

# List specific directory
bft> ls subdir

# Change remote directory
bft> cd subdir
bft> cd ..
bft> cd /

# Print current remote directory
bft> pwd

# Get file/directory info (size, permissions, date)
bft> info myfile.txt
```

**Example `ls` output:**
```
Directory: /
  DIR  drwxr-xr-x    0 B 2026-03-28 21:10 documents
  DIR  drwxr-xr-x    0 B 2026-03-28 21:10 photos
  FILE -rw-r--r--  1.5 KB 2026-03-28 21:15 readme.txt
  FILE -rw-r--r-- 10.00 MB 2026-03-28 21:20 backup.tar.gz
```

#### File Transfer

```bash
# Download file from server to current local directory
bft> download readme.txt

# Download to specific local directory
bft> download readme.txt /home/user/downloads

# Download entire directory
bft> download documents /home/user/downloads

# Upload local file to server (saves to current remote directory)
bft> upload /home/user/report.pdf

# Upload to specific remote path
bft> upload /home/user/report.pdf reports/report.pdf

# Upload entire directory
bft> upload /home/user/project myproject
```

**Transfer progress:**
```
  512.0 KB / 1.00 MB (50.0%) 125.0 Kbps
```

#### Remote Command Execution

```bash
# Execute command on server (output streamed in real-time)
bft> exec uname -a
Linux server 5.10.216-tegra ... aarch64 GNU/Linux

# Shortcut with !
bft> ! df -h /
Filesystem      Size  Used Avail Use% Mounted on
/dev/mmcblk0p1   54G   46G  5.3G  90% /

bft> ! curl -o update.tar.gz https://example.com/update.tar.gz
```

Server must not have `--no-exec` flag. Exit code is shown if non-zero.

#### File Management (on server)

```bash
# Create directory on server
bft> mkdir new_folder

# Delete file or directory on server
bft> rm old_file.txt
bft> rm old_directory

# Copy file/directory on server (server-side, fast)
bft> cp original.txt copy.txt
bft> cp source_dir dest_dir

# Move/rename on server
bft> mv old_name.txt new_name.txt
bft> mv file.txt subdir/file.txt
```

#### Compression

Toggle compression on/off at any time during a session:

```bash
# Toggle compression (on -> off, off -> on)
bft> compress

# Explicitly enable
bft> compress on

# Explicitly disable
bft> compress off
```

When compression is enabled:
- Each data chunk is compressed with **DEFLATE (BestSpeed)** using a streaming compressor that reuses the encoder across chunks for lower CPU overhead (53x faster than per-chunk allocation)
- If compression doesn't reduce the chunk size, it is sent uncompressed automatically
- CRC32 integrity check is always on the **original** (uncompressed) data
- The receiver automatically detects compressed vs uncompressed chunks

**When to use compression:**
| Data type | Compression ratio | Recommendation |
|-----------|------------------|----------------|
| Text files, logs, CSV | 80-95% reduction | **Always use** |
| JSON, XML, HTML | 70-90% reduction | **Always use** |
| Source code | 60-80% reduction | **Always use** |
| Already compressed (ZIP, JPEG, MP4) | ~0% reduction | No benefit |
| Random/encrypted binary | ~0% reduction | No benefit |

**Effective throughput with compression** (CSR8510 at 15 KB/s raw):

| Raw speed | Compression ratio | Effective speed |
|-----------|-------------------|-----------------|
| 15 KB/s | 0% (binary) | 15 KB/s |
| 15 KB/s | 70% (source code) | ~50 KB/s effective |
| 15 KB/s | 90% (text/CSV) | ~150 KB/s effective |

#### Exit

```bash
bft> exit
```

### Web GUI

BFT includes a browser-based file manager for remote file operations.

```bash
# Start web GUI connected to a BFT server
sudo bft web --server 00:1A:7D:DA:71:22 --port 8080 --web-user admin --web-pass secret
```

Open `http://localhost:8080` in a browser. Features:
- Browse, upload, download, and delete files
- Create folders, execute remote commands via terminal
- **Connection status bar** showing live connected/disconnected state with server address
- **Connect/Disconnect button** to manage the Bluetooth connection from the browser
- Transfer progress overlay with speed, elapsed time, and ETA
- Status auto-refreshes every 5 seconds

| Option | Default | Description |
|--------|---------|-------------|
| `--server` | (required) | BFT server Bluetooth address |
| `--port` | `8080` | Web server port |
| `--web-user` | `admin` | Web login username |
| `--web-pass` | `quansu1!` | Web login password |

All connection options (`--channel`, `--rfcomm`, `--user`, `--pass`) are also supported.

### Scanning for Devices

Standalone scan without entering interactive mode:

```bash
sudo bft scan --adapter hci0 --timeout 15
```

---

## Benchmark

Run transfer speed benchmarks:

```bash
# Start server first (terminal 1)
sudo bft server --adapter hci0 --dir /path/to/testfiles --channel 1

# Run benchmark (terminal 2)
sudo bft benchmark \
  --client-adapter hci1 \
  --server 00:1A:7D:DA:71:11 \
  --channel 1 \
  --remote-file testfile.bin \
  --local-dir /tmp/bench

# Upload benchmark
sudo bft benchmark \
  --client-adapter hci1 \
  --server 00:1A:7D:DA:71:11 \
  --channel 1 \
  --upload-file /path/to/localfile.bin

# Both download and upload in one run
sudo bft benchmark \
  --client-adapter hci1 \
  --server 00:1A:7D:DA:71:11 \
  --channel 1 \
  --remote-file testfile.bin \
  --upload-file /path/to/localfile.bin \
  --local-dir /tmp/bench

# Benchmark without compression
sudo bft benchmark \
  --client-adapter hci1 \
  --server 00:1A:7D:DA:71:11 \
  --channel 1 \
  --no-compress \
  --remote-file binary.bin \
  --local-dir /tmp/bench

# Benchmark with L2CAP (Linux only)
sudo bft benchmark \
  --client-adapter hci1 \
  --server 00:1A:7D:DA:71:11 \
  --channel 1 \
  --l2cap \
  --remote-file testfile.bin \
  --local-dir /tmp/bench
```

### MTU Negotiation

At connection time, client and server exchange ACL adapter info (MTU and buffer slots) and agree on an optimal transfer chunk size. This happens automatically and is backward-compatible with older versions.

```
Chunk size = min(client_preferred, server_preferred)
Preferred  = acl_mtu × acl_pkts / 2    (clamped to 1KB–64KB)
```

Adapters with large buffers automatically use larger chunks for higher throughput. Small-buffer adapters (e.g., CSR8510) stay at safe sizes to avoid flow control stalls.

### Pipeline I/O

File transfers use a pipelined architecture with separate reader and writer goroutines:
- **Reader goroutine**: reads file, computes CRC32, compresses (if enabled), encodes protocol messages
- **Writer goroutine**: sends pre-encoded messages over the Bluetooth socket
- 4-chunk send-ahead window overlaps disk I/O with network I/O
- On Bluetooth (where socket writes block for 5-50ms), this provides ~30-50% speedup

### Expected Performance

**RFCOMM transport (default, cross-platform):**

| Adapter | Throughput | Notes |
|---------|-----------|-------|
| CSR8510 A10 | ~15 KB/s (120 Kbps) | DM1 packets, ACL MTU=310 |
| BT 4.0 EDR adapter | ~90 KB/s (720 Kbps) | DH5 packets |
| BT 2.1 EDR adapter | ~150-250 KB/s (1.2-2.0 Mbps) | 3-DH5 packets |

**L2CAP transport (Linux only):**

| Adapter | Throughput | Notes |
|---------|-----------|-------|
| CSR8510 A10 | ~45-60 KB/s (360-480 Kbps) | Lower overhead, larger MTU |
| BT 2.1 EDR adapter | ~200-260 KB/s (1.6-2.1 Mbps) | Near theoretical max |

### RFCOMM vs L2CAP

| | RFCOMM | L2CAP |
|---|---|---|
| **Platform** | Linux + Windows | Linux only |
| **Overhead** | ~9 bytes/packet | ~4 bytes/packet |
| **Max MTU** | 32 KB | 64 KB |
| **Throughput** | Baseline | **3-4x faster** |
| **Setup** | Simple | Simple |
| **Flag** | (default) | `--l2cap` |

Use RFCOMM when you need cross-platform support. Use L2CAP on Linux for maximum throughput.

---

## Troubleshooting

### "Permission denied" or "Operation not permitted"

```bash
# Run with sudo
sudo bft server --dir /shared

# Or set capabilities:
sudo setcap 'cap_net_admin,cap_net_raw+eip' $(which bft)
```

### "No such device" / Adapter not found

```bash
# Check USB devices
lsusb | grep -i bluetooth

# Check kernel modules
lsmod | grep btusb

# Load Bluetooth module if missing
sudo modprobe btusb

# Bring adapter up
sudo hciconfig hci0 up
```

### "Address already in use" on server

Another process is using the RFCOMM channel. Either:

```bash
# Use a different channel
sudo bft server --channel 5

# Or find and kill the process
sudo fuser /dev/rfcomm0
```

### "Connection refused"

- Ensure server is running and using the correct channel
- Ensure server adapter is UP and connectable:
  ```bash
  sudo hciconfig hci0 up
  sudo hciconfig hci0 piscan
  ```
- Verify BD Address matches:
  ```bash
  hciconfig hci0 | grep "BD Address"
  ```

### Both adapters show same BD Address

Common with CSR8510 clones. See [Hardware Setup - Step 2](#step-2-change-bd-address-of-second-adapter).

### Slow transfer speed / Frequent stalls

1. Disable power saving:
   ```bash
   sudo hciconfig hci0 lp RSWITCH
   sudo hciconfig hci1 lp RSWITCH
   ```

2. Enable EDR packet types:
   ```bash
   sudo hciconfig hci0 ptype DM1,DM3,DM5,DH1,DH3,DH5,2-DH1,2-DH3,2-DH5,3-DH1,3-DH3,3-DH5
   ```

3. Check link quality:
   ```bash
   sudo hcitool lq <remote-address>
   # Link quality: 255 = excellent
   ```

4. Check signal strength:
   ```bash
   sudo hcitool rssi <remote-address>
   # RSSI > 0 = good
   ```

5. Move adapters closer together or use USB extension cables to reduce interference.

### "Bluetooth service not running"

```bash
sudo systemctl start bluetooth
sudo systemctl enable bluetooth
```

### Windows: Adapter not detected

1. Open **Device Manager**
2. Check **Bluetooth** section
3. If showing with yellow warning, right-click > **Update driver**
4. Try **Generic Bluetooth Adapter** driver if vendor driver fails
5. Ensure Bluetooth is enabled in **Settings > Devices > Bluetooth**

### Transfer interrupted / Connection dropped

- BFT detects disconnections and reports clean error messages
- **Transfer recovery**: if a transfer fails mid-stream (CRC mismatch, disk error), the protocol stream is automatically drained and re-synchronized. The session remains usable for subsequent commands (ls, download, upload, etc.) without reconnecting
- Partial files are automatically cleaned up on failure
- If the connection drops completely, re-connect and retry the transfer
- If frequent drops occur, reduce distance between adapters or check for USB bandwidth contention (avoid using USB hubs)
