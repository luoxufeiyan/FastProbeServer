#!/bin/bash
set -e

# FastProbe Client Installation Script

# Define installation paths
BIN_DIR="/usr/local/bin"
CONF_DIR="/etc/fastprobe-client"
CONF_FILE="$CONF_DIR/config.json"
SERVICE_FILE="/etc/systemd/system/fastprobe-client.service"
BIN_NAME="fastprobe-client"

echo "====================================="
echo " FastProbe Client Installation"
echo "====================================="

# Check for root privileges
if [ "$EUID" -ne 0 ]; then
  echo "Please run as root (or using sudo)."
  exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)
    RELEASE_ARCH="amd64"
    ;;
  aarch64|arm64)
    RELEASE_ARCH="arm64"
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

OS="linux"

# Find latest release
echo "Fetching latest release information..."
REPO="luoxufeiyan/FastProbeServer"
LATEST_URL=$(curl -s https://api.github.com/repos/$REPO/releases/latest | grep "browser_download_url" | grep "fastprobe-client-$OS-$RELEASE_ARCH" | cut -d '"' -f 4)

if [ -z "$LATEST_URL" ]; then
    # Fallback to constructing URL if github API rate limited or no release yet
    echo "Could not find latest release via API, assuming v1.0.0 for fallback (you may need to install manually if this fails)."
    LATEST_URL="https://github.com/$REPO/releases/latest/download/fastprobe-client-$OS-$RELEASE_ARCH"
fi

echo "Downloading $LATEST_URL..."
curl -L -o "$BIN_DIR/$BIN_NAME" "$LATEST_URL"
chmod +x "$BIN_DIR/$BIN_NAME"

echo "Binary installed to $BIN_DIR/$BIN_NAME"

# Prompt for configuration
echo "-------------------------------------"
echo " Configuration"
echo "-------------------------------------"
read -p "Enter FastProbe Server URL (e.g., https://status.yourdomain.com/report): " SERVER_URL
read -p "Enter your Node Token: " NODE_TOKEN

mkdir -p "$CONF_DIR"

# Create config.json
cat > "$CONF_FILE" <<EOF
{
  "url": "$SERVER_URL",
  "token": "$NODE_TOKEN"
}
EOF
echo "Configuration saved to $CONF_FILE"

# Create systemd service
echo "Setting up systemd service..."

cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=FastProbe Client Service
After=network.target

[Service]
Type=simple
User=root
ExecStart=$BIN_DIR/$BIN_NAME -config $CONF_FILE
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable fastprobe-client
systemctl start fastprobe-client

echo "====================================="
echo " Installation Complete!"
echo " FastProbe Client is now running."
echo " You can check the status with: systemctl status fastprobe-client"
echo "====================================="
