# VPS Provisioning Runbook

Target: Hetzner CX22 (2 vCPU, 4 GB RAM, Ubuntu 24.04) — Falkenstein or Helsinki datacenter for lowest latency to Binance EU endpoints. Cost: ~€4.51/mo.

---

## 1. Create the VPS

1. Sign up at [hetzner.com](https://www.hetzner.com/cloud), create a **CX22** server.
   - Region: **Falkenstein** (FSN1) or **Helsinki** (HEL1)
   - Image: **Ubuntu 24.04**
   - Add your SSH public key during creation.
2. Note the server's public IP.

---

## 2. Initial server hardening

```bash
ssh root@<IP>

# Create a non-root deploy user
useradd -m -s /bin/bash paperlive
usermod -aG sudo paperlive

# Disable root SSH login
sed -i 's/^PermitRootLogin yes/PermitRootLogin no/' /etc/ssh/sshd_config
systemctl reload sshd

# Basic firewall — allow only SSH
apt install -y ufw
ufw allow 22/tcp
ufw --force enable
```

---

## 3. Install dependencies

```bash
# Go 1.22+ — use the official tarball if apt's version is too old
apt install -y git jq logrotate curl

# Check apt Go version
apt show golang-go 2>/dev/null | grep Version

# If version < 1.22, install from upstream:
curl -fsSL https://go.dev/dl/go1.22.5.linux-amd64.tar.gz -o /tmp/go.tar.gz
tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh
source /etc/profile.d/go.sh
```

---

## 4. Deploy the engine

```bash
# As paperlive
su - paperlive

git clone https://github.com/cristianmanoliu/trading-engine /opt/trading-engine
cd /opt/trading-engine

# Build all binaries
go build -o bin/engine       ./cmd/engine
go build -o bin/backtest     ./cmd/backtest
go build -o bin/journal_report ./cmd/journal_report
```

---

## 5. Configure credentials and log directories

```bash
# Back as root
mkdir -p /etc/paper-live /var/log/paper-live/journal /var/lib/paper-live

cat > /etc/paper-live/env <<'EOF'
TELEGRAM_BOT_TOKEN=<your_bot_token>
TELEGRAM_CHAT_ID=<your_chat_id>
EOF

chmod 600 /etc/paper-live/env
chown paperlive:paperlive /etc/paper-live/env
chown -R paperlive:paperlive /var/log/paper-live /var/lib/paper-live
```

---

## 6. Install systemd units and logrotate

```bash
# systemd units
cp /opt/trading-engine/deploy/systemd/* /etc/systemd/system/

# logrotate
cp /opt/trading-engine/deploy/logrotate.d/paper-live /etc/logrotate.d/paper-live

systemctl daemon-reload
```

---

## 7. Enable and start everything

```bash
# Enable all 8 engine instances
systemctl enable \
    paper-live@btcusdt \
    paper-live@ethusdt \
    paper-live@bnbusdt \
    paper-live@solusdt \
    paper-live@xrpusdt \
    paper-live@dogeusdt \
    paper-live@linkusdt \
    paper-live@ltcusdt

# Enable the group target and timer units
systemctl enable paper-live.target paper-live-watchdog.timer paper-live-digest.timer

# Start just BTC first for pre-flight check (see below)
systemctl start paper-live@btcusdt
```

---

## 8. Pre-flight verification (run before enabling all 8)

Complete these 7 checks in order. Stop and fix if any fail before proceeding.

| # | Check | Pass criterion |
|---|-------|----------------|
| 1 | **Backfill** | `journalctl -u paper-live@btcusdt -f` shows `"kline backfill complete" hours=48 ticks~=11520` within 5s |
| 2 | **Telegram startup** | Telegram receives `🟢 BTCUSDT engine started on …` within 30s |
| 3 | **PDH/PDL primed** | `systemctl restart paper-live@btcusdt`; after 60s restart, `levels.HasData()` true (no `"levels not ready"` in log) |
| 4 | **Heartbeat** | `journalctl -u paper-live@btcusdt --since "3 min ago" \| grep heartbeat` shows ≥ 2 lines |
| 5 | **Journal durability** | Wait for a signal or 30 min; then `kill -9 $(systemctl show -p MainPID --value paper-live@btcusdt)`; verify `/var/log/paper-live/journal/BTCUSDT-*.jsonl` is intact after auto-restart |
| 6 | **Watchdog alert** | `systemctl stop paper-live@btcusdt`; within 10 min Telegram warns `🔴 Watchdog: BTCUSDT process is DEAD`; restart it after |
| 7 | **Multi-symbol** | `systemctl start paper-live.target`; all 8 Telegram startup messages arrive; after 5 min `bash /opt/trading-engine/scripts/paper_live_status.sh` shows 8 rows with STATUS=ALIVE |

Only after all 7 pass: enable the digest timer and start the 21-day window.

```bash
systemctl start paper-live-watchdog.timer paper-live-digest.timer
```

---

## Useful commands

```bash
# Check status of all engines
bash /opt/trading-engine/scripts/paper_live_status.sh

# Follow logs for one symbol
journalctl -u paper-live@btcusdt -f

# Check all logs for WS gaps
grep '"ws gap closed"' /var/log/paper-live/*.log | wc -l

# Run reconciliation (compares paper-live journals vs backtest)
bash /opt/trading-engine/scripts/paper_live_report.sh

# Stop all engines gracefully
systemctl stop paper-live.target

# Disk usage sanity check (should stay < 2 GB over 21 days)
du -sh /var/log/paper-live/
```

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Watchdog fires immediately after start | Process crashes on startup | `journalctl -u paper-live@<symbol> -n 50` — look for config error |
| 0 heartbeats after 10 min | Engine goroutine stuck in backfill | Check REST backfill log; `fapi.binance.com` may be rate-limiting |
| High WS gap count | Network or Binance connectivity | `ping fapi.binance.com` from VPS; consider switching datacenter |
| Journal file 0 bytes | `PAPER_LIVE_JOURNAL_DIR` not writable | `chown paperlive:paperlive /var/log/paper-live/journal` |
| No Telegram messages | Credentials wrong | `curl -sS "https://api.telegram.org/bot<TOKEN>/getMe"` to validate token |
