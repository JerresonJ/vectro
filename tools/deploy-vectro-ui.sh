#!/usr/bin/env bash
# Install the Vectro web UI (vectro-ui) on the robot over SSH, in place of
# WireOS's wired at /usr/bin/wired (same service, same port 8080).
#
#   tools/deploy-vectro-ui.sh [robot-ip]            install vectro-ui/build/vectro-ui
#   tools/deploy-vectro-ui.sh [robot-ip] --restore  put WireOS's wired back
#
# The WireOS original is saved once to /data/vectro/backup/wired.wireos.
set -euo pipefail

ROBOT=${1:-10.0.0.215}
MODE=${2:-}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
KEY=$ROOT/keys/vector_root_key
BIN=$ROOT/vectro-ui/build/vectro-ui
SSH_OPTS=(-i "$KEY" -o PubkeyAcceptedAlgorithms=+ssh-rsa -o HostKeyAlgorithms=+ssh-rsa -o ConnectTimeout=8)

if [ "$MODE" != "--restore" ]; then
  [ -f "$BIN" ] || { echo "missing $BIN - build it first" >&2; exit 1; }
  ssh "${SSH_OPTS[@]}" root@"$ROBOT" 'mkdir -p /data/vectro/backup'
  scp -O "${SSH_OPTS[@]}" "$BIN" root@"$ROBOT":/data/vectro/wired.new
  SRC=/data/vectro/wired.new
else
  SRC=/data/vectro/backup/wired.wireos
fi

ssh "${SSH_OPTS[@]}" root@"$ROBOT" SRC="$SRC" sh -s <<'EOF'
set -e
[ -f "$SRC" ] || { echo "missing $SRC on robot" >&2; exit 1; }
[ -f /data/vectro/backup/wired.wireos ] || cp -p /usr/bin/wired /data/vectro/backup/wired.wireos
systemctl stop wired
mount -o remount,rw /
cp "$SRC" /usr/bin/wired
chmod 0755 /usr/bin/wired
sync
mount -o remount,ro /
systemctl start wired
sleep 5
systemctl is-active wired
EOF
