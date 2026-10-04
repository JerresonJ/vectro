#!/usr/bin/env bash
# Install Vectro's vic-cloud on the robot over SSH.
#
#   tools/deploy-vic-cloud.sh [robot-ip]            install vectro-cloud/build/vic-cloud
#   tools/deploy-vic-cloud.sh [robot-ip] --restore  put the WireOS original back
#
# The WireOS original is saved once to /data/vectro/backup/vic-cloud.wireos.
set -euo pipefail

ROBOT=${1:-10.0.0.215}
MODE=${2:-}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
KEY=$ROOT/keys/vector_root_key
BIN=$ROOT/vectro-cloud/build/vic-cloud
SSH_OPTS=(-i "$KEY" -o PubkeyAcceptedAlgorithms=+ssh-rsa -o HostKeyAlgorithms=+ssh-rsa -o ConnectTimeout=8)

if [ "$MODE" != "--restore" ]; then
  [ -f "$BIN" ] || { echo "missing $BIN - build it first" >&2; exit 1; }
  ssh "${SSH_OPTS[@]}" root@"$ROBOT" 'mkdir -p /data/vectro/backup'
  scp -O "${SSH_OPTS[@]}" "$BIN" root@"$ROBOT":/data/vectro/vic-cloud.new
  SRC=/data/vectro/vic-cloud.new
else
  SRC=/data/vectro/backup/vic-cloud.wireos
fi

ssh "${SSH_OPTS[@]}" root@"$ROBOT" SRC="$SRC" sh -s <<'EOF'
set -e
[ -f "$SRC" ] || { echo "missing $SRC on robot" >&2; exit 1; }
[ -f /data/vectro/backup/vic-cloud.wireos ] || cp -p /anki/bin/vic-cloud /data/vectro/backup/vic-cloud.wireos
systemctl stop vic-cloud
mount -o remount,rw /
cp "$SRC" /anki/bin/vic-cloud
chown cloud:anki /anki/bin/vic-cloud
chmod 0550 /anki/bin/vic-cloud
sync
mount -o remount,ro /
# vic-switchboard keeps its datagram socket connected to the old vic-cloud
# (connect fails with EPERM), so restart it with vic-cloud. The engine, anim
# and robot processes reconnect to the new vic-cloud on their own; restarting
# them in place races, and a soft reboot has left the IMU confused.
systemctl restart vic-switchboard
systemctl start vic-cloud
for i in $(seq 1 30); do
  journalctl -u vic-cloud --since "-60s" --no-pager | grep -q "Listening on Port: 443" && break
  sleep 2
done
systemctl is-active vic-cloud vic-switchboard vic-engine vic-anim
EOF
