#!/bin/bash
# install-web.sh — Install BFT Web GUI client as a systemd service
#
# The service runs as root for Bluetooth adapter control (HCI ioctls)
# and connects to a BFT server, exposing a web file manager.
#
# Usage:
#   sudo ./scripts/install-web.sh [options]
#
# Options:
#   --server <bt-addr>      BFT server Bluetooth address (required)
#   --adapter <hci>         Bluetooth adapter (default: hci0)
#   --channel <n>           Channel number (default: 1)
#   --port <n>              Web server port (default: 8080)
#   --web-user <user>       Web login username (default: admin)
#   --web-pass <pass>       Web login password (default: admin)
#   --bt-user <user>        BFT server auth username (default: none)
#   --bt-pass <pass>        BFT server auth password (default: none)
#   --rfcomm                Use RFCOMM transport (default: L2CAP)
#   --uninstall             Remove service
#
# Examples:
#   sudo ./scripts/install-web.sh --server 00:1A:7D:DA:71:11
#   sudo ./scripts/install-web.sh --server 00:1A:7D:DA:71:11 --port 3000 --web-user admin --web-pass secret
#   sudo ./scripts/install-web.sh --server 00:1A:7D:DA:71:11 --bt-user admin --bt-pass mypass
#   sudo ./scripts/install-web.sh --uninstall

set -euo pipefail

BINARY_NAME="bft"
INSTALL_DIR="/usr/local/bin"
SERVICE_NAME="bft-web"
CONF_DIR="/etc/bft"

# Defaults
SERVER_ADDR=""
ADAPTER="hci0"
CHANNEL="1"
PORT="8080"
WEB_USER="admin"
WEB_PASS="admin"
BT_USER=""
BT_PASS=""
USE_RFCOMM=false
UNINSTALL=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --server)    SERVER_ADDR="$2"; shift 2 ;;
        --adapter)   ADAPTER="$2"; shift 2 ;;
        --channel)   CHANNEL="$2"; shift 2 ;;
        --port)      PORT="$2"; shift 2 ;;
        --web-user)  WEB_USER="$2"; shift 2 ;;
        --web-pass)  WEB_PASS="$2"; shift 2 ;;
        --bt-user)   BT_USER="$2"; shift 2 ;;
        --bt-pass)   BT_PASS="$2"; shift 2 ;;
        --rfcomm)    USE_RFCOMM=true; shift ;;
        --uninstall) UNINSTALL=true; shift ;;
        -h|--help)   head -28 "$0" | tail -27; exit 0 ;;
        *)           echo "Unknown option: $1"; exit 1 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: must run as root (use sudo)"
    exit 1
fi

# --- Uninstall ---
if [ "$UNINSTALL" = true ]; then
    echo "Uninstalling BFT Web..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
    rm -f "${CONF_DIR}/web-env"
    systemctl daemon-reload
    echo "Removed service. Binary '${INSTALL_DIR}/${BINARY_NAME}' was NOT removed (may be shared with bft-server)."
    exit 0
fi

# --- Validate ---
if [ -z "$SERVER_ADDR" ]; then
    echo "Error: --server <bt-address> is required"
    echo "Example: sudo $0 --server 00:1A:7D:DA:71:11"
    exit 1
fi

# Install binary if not already present
if [ ! -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
    BINARY_SRC=""
    for candidate in "./$BINARY_NAME" "./bft-linux-amd64" "./bft-linux-arm64"; do
        if [ -f "$candidate" ]; then
            BINARY_SRC="$candidate"
            break
        fi
    done
    if [ -z "$BINARY_SRC" ]; then
        echo "Error: BFT binary not found. Build first: make linux"
        exit 1
    fi
    install -m 755 "$BINARY_SRC" "${INSTALL_DIR}/${BINARY_NAME}"
    echo "[1/3] Binary installed -> ${INSTALL_DIR}/${BINARY_NAME}"
else
    echo "[1/3] Binary already at ${INSTALL_DIR}/${BINARY_NAME}"
fi

echo ""
echo "=== Installing BFT Web GUI ==="
echo "  Server:    $SERVER_ADDR"
echo "  Adapter:   $ADAPTER"
echo "  Channel:   $CHANNEL"
echo "  Web port:  $PORT"
echo "  Web auth:  $WEB_USER / ****"
echo "  BT auth:   $([ -n "$BT_USER" ] && echo "$BT_USER / ****" || echo disabled)"
echo "  Transport: $([ "$USE_RFCOMM" = true ] && echo RFCOMM || echo L2CAP)"
echo ""

# Build ExecStart command
EXEC_ARGS="web --server ${SERVER_ADDR} --adapter ${ADAPTER} --channel ${CHANNEL} --port ${PORT} --web-user ${WEB_USER} --web-pass ${WEB_PASS}"
if [ "$USE_RFCOMM" = true ]; then
    EXEC_ARGS="$EXEC_ARGS --rfcomm"
fi
if [ -n "$BT_USER" ]; then
    EXEC_ARGS="$EXEC_ARGS --user ${BT_USER}"
fi
if [ -n "$BT_PASS" ]; then
    EXEC_ARGS="$EXEC_ARGS --pass ${BT_PASS}"
fi

# Create config directory
mkdir -p "$CONF_DIR"

# Save environment for reference
cat > "${CONF_DIR}/web-env" <<ENVFILE
# BFT Web environment — reference only (credentials stored in systemd unit)
BFT_SERVER=${SERVER_ADDR}
BFT_ADAPTER=${ADAPTER}
BFT_CHANNEL=${CHANNEL}
BFT_WEB_PORT=${PORT}
BFT_WEB_USER=${WEB_USER}
ENVFILE
chmod 600 "${CONF_DIR}/web-env"

# Create systemd service
#
# Runs as root so BFT can:
#   - Open HCI sockets for adapter control (ensureAdapterUp)
#   - Open RFCOMM/L2CAP sockets (AF_BLUETOOTH)
#
# The web server binds to the specified port and proxies
# file operations to the remote BFT server via Bluetooth.
# No local file I/O (downloads use temp files cleaned up automatically).
#
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Blue File Transfer Web GUI
Documentation=https://github.com/binhfile/blue-file-transfer
After=bluetooth.target network.target
Wants=bluetooth.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/${BINARY_NAME} ${EXEC_ARGS}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

# Security hardening
ProtectHome=read-only
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
ProtectSystem=strict
# Web needs /tmp for upload staging
ReadWritePaths=/tmp

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
echo "[2/3] Systemd service '${SERVICE_NAME}' created"

# Enable and start
echo "[3/3] Service ready"

echo ""
echo "=== Installation complete ==="
echo ""
echo "Commands:"
echo "  sudo systemctl start $SERVICE_NAME        # Start web GUI"
echo "  sudo systemctl enable $SERVICE_NAME        # Auto-start on boot"
echo "  sudo systemctl status $SERVICE_NAME        # Check status"
echo "  sudo journalctl -u $SERVICE_NAME -f        # View logs"
echo "  sudo systemctl stop $SERVICE_NAME          # Stop web GUI"
echo ""
echo "Open in browser: http://$(hostname -I | awk '{print $1}'):${PORT}"
echo "  Login: ${WEB_USER} / ****"
echo ""
echo "To uninstall: sudo $0 --uninstall"
