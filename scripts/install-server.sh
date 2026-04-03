#!/bin/bash
# install-server.sh — Install BFT server as a systemd service
#
# The service runs as root for Bluetooth adapter control (HCI ioctls)
# but drops to a normal user for all file I/O via BFT's --dir flag.
#
# Usage:
#   sudo ./scripts/install-server.sh [options]
#
# Options:
#   --adapter <hci>         Bluetooth adapter (default: hci0)
#   --dir <path>            Directory to serve (default: /srv/bft)
#   --channel <n>           Channel number (default: 1)
#   --file-user <user>      Owner of served files (default: bft)
#   --max-clients <n>       Max concurrent connections (default: 5)
#   --users-file <path>     Auth users file (default: none, no auth)
#   --allow-exec            Enable remote command execution
#   --rfcomm                Use RFCOMM transport (default: L2CAP)
#   --uninstall             Remove service and binary
#
# Examples:
#   sudo ./scripts/install-server.sh --dir /home/pi/shared --file-user pi
#   sudo ./scripts/install-server.sh --dir /srv/bft --users-file /etc/bft/users.json
#   sudo ./scripts/install-server.sh --uninstall

set -euo pipefail

BINARY_NAME="bft"
INSTALL_DIR="/usr/local/bin"
SERVICE_NAME="bft-server"
CONF_DIR="/etc/bft"

# Defaults
ADAPTER="hci0"
SERVE_DIR="/srv/bft"
CHANNEL="1"
FILE_USER="${SUDO_USER:-bft}"
MAX_CLIENTS="5"
USERS_FILE=""
ALLOW_EXEC=false
USE_RFCOMM=false
UNINSTALL=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --adapter)      ADAPTER="$2"; shift 2 ;;
        --dir)          SERVE_DIR="$2"; shift 2 ;;
        --channel)      CHANNEL="$2"; shift 2 ;;
        --file-user)    FILE_USER="$2"; shift 2 ;;
        --max-clients)  MAX_CLIENTS="$2"; shift 2 ;;
        --users-file)   USERS_FILE="$2"; shift 2 ;;
        --allow-exec)   ALLOW_EXEC=true; shift ;;
        --rfcomm)       USE_RFCOMM=true; shift ;;
        --uninstall)    UNINSTALL=true; shift ;;
        -h|--help)      head -25 "$0" | tail -24; exit 0 ;;
        *)              echo "Unknown option: $1"; exit 1 ;;
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
    echo "Note: user '$FILE_USER', directory '$SERVE_DIR', and config '$CONF_DIR' were NOT removed."
    exit 0
fi

# --- Find binary ---
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

echo "=== Installing BFT Server ==="
echo "  Binary:      $BINARY_SRC"
echo "  Adapter:     $ADAPTER"
echo "  Directory:   $SERVE_DIR"
echo "  File owner:  $FILE_USER"
echo "  Channel:     $CHANNEL"
echo "  Max clients: $MAX_CLIENTS"
echo "  Transport:   $([ "$USE_RFCOMM" = true ] && echo RFCOMM || echo L2CAP)"
echo "  Auth:        $([ -n "$USERS_FILE" ] && echo "$USERS_FILE" || echo disabled)"
echo "  Exec:        $([ "$ALLOW_EXEC" = true ] && echo enabled || echo disabled)"
echo ""

# 1. Install binary
install -m 755 "$BINARY_SRC" "${INSTALL_DIR}/${BINARY_NAME}"
echo "[1/5] Binary installed -> ${INSTALL_DIR}/${BINARY_NAME}"

# 2. Create file-owner user (for file I/O only, not for running the service)
if ! id "$FILE_USER" &>/dev/null; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "$FILE_USER"
    echo "[2/5] User '$FILE_USER' created"
else
    echo "[2/5] User '$FILE_USER' already exists"
fi

# 3. Create serve directory owned by file user
mkdir -p "$SERVE_DIR"
chown "${FILE_USER}:${FILE_USER}" "$SERVE_DIR"
chmod 775 "$SERVE_DIR"
echo "[3/5] Directory '$SERVE_DIR' ready (owned by $FILE_USER)"

# 4. Create config directory and users file if needed
mkdir -p "$CONF_DIR"
if [ -n "$USERS_FILE" ] && [ ! -f "$USERS_FILE" ]; then
    touch "$USERS_FILE"
    chmod 600 "$USERS_FILE"
    echo "[4/5] Users file created: $USERS_FILE"
    echo "  Add users: bft useradd --users-file $USERS_FILE --user <name> --pass <password>"
else
    echo "[4/5] Config directory ready"
fi

# 5. Build ExecStart command
EXEC_ARGS="server --adapter ${ADAPTER} --dir ${SERVE_DIR} --channel ${CHANNEL} --max-clients ${MAX_CLIENTS} --file-user ${FILE_USER}"
if [ "$USE_RFCOMM" = true ]; then
    EXEC_ARGS="$EXEC_ARGS --rfcomm"
fi
if [ -n "$USERS_FILE" ]; then
    EXEC_ARGS="$EXEC_ARGS --users-file ${USERS_FILE}"
fi
if [ "$ALLOW_EXEC" = false ]; then
    EXEC_ARGS="$EXEC_ARGS --no-exec"
fi

# 6. Create systemd service
#
# Runs as root so BFT can:
#   - Open HCI sockets (BTPROTO_HCI) for adapter control
#   - Run HCIDEVUP / HCISETSCAN ioctls to bring adapter up + enable piscan
#   - Open RFCOMM/L2CAP sockets (AF_BLUETOOTH)
#
# File I/O runs as the file-user via a pre-exec chown wrapper.
# BFT's ensureAdapterUp() handles adapter configuration automatically.
#
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Blue File Transfer Server
Documentation=https://github.com/binhfile/blue-file-transfer
After=bluetooth.target
Wants=bluetooth.target

[Service]
Type=simple

# Run as root for Bluetooth HCI access.
# File operations are scoped to --dir which is owned by ${FILE_USER}.
ExecStart=${INSTALL_DIR}/${BINARY_NAME} ${EXEC_ARGS}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

# Ensure serve directory is writable by file user before start
ExecStartPre=/bin/chown -R ${FILE_USER}:${FILE_USER} ${SERVE_DIR}

# Security hardening — restrict root's reach
ProtectHome=read-only
PrivateTmp=true
NoNewPrivileges=false
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true

# Limit writable paths
ReadWritePaths=${SERVE_DIR}
$([ -n "$USERS_FILE" ] && echo "ReadWritePaths=$(dirname "$USERS_FILE")")

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
echo "[5/5] Systemd service '${SERVICE_NAME}' created"

# Create a helper script to drop privileges for file creation
cat > "${CONF_DIR}/bft-env" <<ENVFILE
# BFT Server environment — sourced by systemd service
BFT_ADAPTER=${ADAPTER}
BFT_DIR=${SERVE_DIR}
BFT_CHANNEL=${CHANNEL}
BFT_FILE_USER=${FILE_USER}
BFT_MAX_CLIENTS=${MAX_CLIENTS}
ENVFILE

echo ""
echo "=== Installation complete ==="
echo ""
echo "Commands:"
echo "  sudo systemctl start $SERVICE_NAME        # Start server"
echo "  sudo systemctl enable $SERVICE_NAME        # Auto-start on boot"
echo "  sudo systemctl status $SERVICE_NAME        # Check status"
echo "  sudo journalctl -u $SERVICE_NAME -f        # View logs"
echo "  sudo systemctl stop $SERVICE_NAME          # Stop server"
echo ""
echo "File management:"
echo "  Files in '$SERVE_DIR' are owned by '$FILE_USER'."
echo "  To add files as that user:  sudo -u $FILE_USER cp myfile $SERVE_DIR/"
echo "  Or add your user to the group: sudo usermod -aG $FILE_USER \$USER"
echo ""
if [ -n "$USERS_FILE" ]; then
    echo "Authentication:"
    echo "  bft useradd --users-file $USERS_FILE --user admin --pass <password>"
    echo ""
fi
echo "To uninstall: sudo $0 --uninstall"
