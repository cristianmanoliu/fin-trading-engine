#!/usr/bin/env bash
# Run as root on a fresh Ubuntu 24.04 VPS.
# Sets up: paperlive user, SSH hardening, firewall, Go 1.22+, deps.
set -euo pipefail

if [[ "$(uname)" != "Linux" ]]; then
    echo "Error: this script must run on the Linux VPS, not locally." >&2
    echo "Run it remotely: ssh root@<VPS_IP> 'bash -s' < ./deploy/setup.sh" >&2
    exit 1
fi

GO_MIN="1.22"

# ── User ──────────────────────────────────────────────────────────────────────
useradd -m -s /bin/bash paperlive 2>/dev/null || echo "paperlive user already exists"

# ── SSH hardening ─────────────────────────────────────────────────────────────
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
systemctl reload ssh 2>/dev/null || systemctl reload sshd 2>/dev/null || true

# ── Firewall ──────────────────────────────────────────────────────────────────
apt-get install -y ufw
ufw allow 22/tcp
ufw --force enable

# ── Base dependencies ─────────────────────────────────────────────────────────
apt-get update
apt-get install -y git jq logrotate curl

# ── Go ────────────────────────────────────────────────────────────────────────
install_go_upstream() {
    local ver="1.22.5"
    echo "→ Installing Go ${ver} from upstream..."
    curl -fsSL "https://go.dev/dl/go${ver}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz
    echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
    export PATH="$PATH:/usr/local/go/bin"
}

apt_go_ver=$(apt-cache show golang-go 2>/dev/null | awk '/^Version:/{print $2; exit}' | grep -oP '^\d+\.\d+' || echo "0")
if awk "BEGIN{exit !($apt_go_ver >= $GO_MIN)}"; then
    apt-get install -y golang-go
    echo "→ Go ${apt_go_ver} installed via apt"
else
    install_go_upstream
fi

go version
echo ""
echo "✓ Setup complete. Next: clone the repo and build binaries."
echo "  su - paperlive"
echo "  git clone https://github.com/cristianmanoliu/trading-engine /opt/trading-engine"
echo "  cd /opt/trading-engine && go build -o bin/engine ./cmd/engine && go build -o bin/backtest ./cmd/backtest && go build -o bin/journal_report ./cmd/journal_report"
