#!/bin/bash
# install-server.sh — Install and configure BFT as a systemd service
#
# Usage:
#   sudo ./install-server.sh [options]
#
# Options:
#   --adapter <hci>     Bluetooth adapter (default: hci0)
#   --dir <path>        Directory to serve (default: /srv/bft)
#   --channel <n>       RFCOMM channel (default: 1)
#   --user <user>       Run as user (default: bft)
#   --uninstall         Remove service, user and binary

set -euo pipefail

BINARY_NAME="bft"
INSTALL_DIR="/usr/local/bin"
SERVICE_NAME="bft-server"
DEFAULT_ADAPTER="hci0"
DEFAULT_DIR="/srv/bft"
DEFAULT_CHANNEL="1"
DEFAULT_USER="bft"
UNINSTALL=false

# Parse arguments
ADAPTER="$DEFAULT_ADAPTER"
SERVE_DIR="$DEFAULT_DIR"
CHANNEL="$DEFAULT_CHANNEL"
RUN_USER="$DEFAULT_USER"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --adapter)   ADAPTER="$2"; shift 2 ;;
        --dir)       SERVE_DIR="$2"; shift 2 ;;
        --channel)   CHANNEL="$2"; shift 2 ;;
        --user)      RUN_USER="$2"; shift 2 ;;
        --uninstall) UNINSTALL=true; shift ;;
        -h|--help)
            head -12 "$0" | tail -11
            exit 0
            ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: must run as root (use sudo)"
    exit 1
fi

# --- Uninstall ---
if [ "$UNINSTALL" = true ]; then
    echo "Uninstalling BFT server..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
    systemctl daemon-reload
    rm -f "${INSTALL_DIR}/${BINARY_NAME}"
    echo "Removed service and binary."
    echo "Note: user '$RUN_USER' and directory '$SERVE_DIR' were NOT removed."
    echo "  To remove: userdel $RUN_USER && rm -rf $SERVE_DIR"
    exit 0
fi

# --- Install ---

# Check binary exists in current directory
if [ ! -f "./$BINARY_NAME" ] && [ ! -f "./bft-linux-amd64" ]; then
    echo "Error: '$BINARY_NAME' or 'bft-linux-amd64' not found in current directory."
    echo "Build first: make linux"
    exit 1
fi

BINARY_SRC="./$BINARY_NAME"
if [ -f "./bft-linux-amd64" ]; then
    BINARY_SRC="./bft-linux-amd64"
fi

echo "=== Installing BFT Server ==="
echo "  Binary:  $BINARY_SRC -> ${INSTALL_DIR}/${BINARY_NAME}"
echo "  Adapter: $ADAPTER"
echo "  Dir:     $SERVE_DIR"
echo "  Channel: $CHANNEL"
echo "  User:    $RUN_USER"
echo ""

# 1. Install binary
install -m 755 "$BINARY_SRC" "${INSTALL_DIR}/${BINARY_NAME}"
echo "[1/5] Binary installed"

# 2. Set capabilities (allows BT access without root)
setcap 'cap_net_admin,cap_net_raw+eip' "${INSTALL_DIR}/${BINARY_NAME}"
echo "[2/5] Capabilities set (cap_net_admin,cap_net_raw)"

# 3. Create service user
if ! id "$RUN_USER" &>/dev/null; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "$RUN_USER"
    echo "[3/5] User '$RUN_USER' created"
else
    echo "[3/5] User '$RUN_USER' already exists"
fi

# 4. Create serve directory
mkdir -p "$SERVE_DIR"
chown "$RUN_USER:$RUN_USER" "$SERVE_DIR"
chmod 755 "$SERVE_DIR"
echo "[4/5] Directory '$SERVE_DIR' ready"

# 5. Create systemd service
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Blue File Transfer Server
Documentation=https://github.com/binhfile/blue-file-transfer
After=bluetooth.target
Requires=bluetooth.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_USER}
ExecStartPre=/usr/bin/hciconfig ${ADAPTER} up
ExecStartPre=/usr/bin/hciconfig ${ADAPTER} piscan
ExecStart=${INSTALL_DIR}/${BINARY_NAME} server --adapter ${ADAPTER} --dir ${SERVE_DIR} --channel ${CHANNEL}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${SERVE_DIR}
PrivateTmp=true

# Bluetooth needs access to /sys and device nodes
DeviceAllow=/dev/null rw

# Allow ambient capabilities for BT socket access
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
echo "[5/5] Systemd service created"

echo ""
echo "=== Installation complete ==="
echo ""
echo "Commands:"
echo "  sudo systemctl start $SERVICE_NAME      # Start server"
echo "  sudo systemctl enable $SERVICE_NAME      # Auto-start on boot"
echo "  sudo systemctl status $SERVICE_NAME      # Check status"
echo "  sudo journalctl -u $SERVICE_NAME -f      # View logs"
echo "  sudo systemctl stop $SERVICE_NAME        # Stop server"
echo ""
echo "Place files in '$SERVE_DIR' to share via Bluetooth."
echo "Connect from client: bft client -> connect <address> $CHANNEL"
