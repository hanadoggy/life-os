#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# LifeOS GCE VM Bootstrap Script (Ubuntu 24.04 LTS)
# ==============================================================================

echo "[1/5] Setting timezone to Asia/Tokyo..."
sudo timedatectl set-timezone Asia/Tokyo

echo "[2/5] Configuring 1GB swap file (if not already present)..."
if [ ! -f /swapfile ]; then
  sudo fallocate -l 1G /swapfile
  sudo chmod 600 /swapfile
  sudo mkswap /swapfile
  sudo swapon /swapfile
  echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
fi

echo "[3/5] Installing Docker Engine & Compose plugin from official apt repository..."
if ! command -v docker >/dev/null 2>&1; then
  sudo apt-get update
  sudo apt-get install -y ca-certificates curl gnupg
  sudo install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  sudo chmod a+r /etc/apt/keyrings/docker.gpg

  echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
    sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

  sudo apt-get update
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi

echo "[4/5] Applying Docker daemon.json log rotation..."
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json > /dev/null <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
EOF
sudo systemctl enable docker
sudo systemctl restart docker
sudo usermod -aG docker "$USER" || true

echo "[5/5] Creating /opt/lifeos application directory..."
sudo mkdir -p /opt/lifeos
sudo chown -R "$USER:$USER" /opt/lifeos

echo "Bootstrap complete! Re-login (or run 'newgrp docker') for docker group membership to take effect."

