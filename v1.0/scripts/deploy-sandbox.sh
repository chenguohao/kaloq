#!/usr/bin/env bash
# Run from the formal workspace. Builds only v1.0; never reads vendor credentials.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOST="${KALOQ_TEST_HOST:-root@8.222.135.101}"
RELEASE="${KALOQ_RELEASE:-$(date -u +%Y%m%dT%H%M%SZ)}"
[[ "$RELEASE" =~ ^[a-zA-Z0-9_-]+$ ]] || exit 2
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
(cd "$ROOT/apps/web" && python3 "$ROOT/scripts/with-toolchain.py" npm run build -- --base=/kaloq-v1/ --outDir "$TMP/web")
(cd "$ROOT/apps/api" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 python3 "$ROOT/scripts/with-toolchain.py" go build -trimpath -o "$TMP/kaloq" ./cmd/kaloq)
mkdir -p "$TMP/database"
cp -R "$ROOT/database/migrations" "$TMP/database/"
cp -R "$ROOT/deploy" "$TMP/deploy"
ssh -o BatchMode=yes "$HOST" "test -f /opt/kaloq-v1/runtime.env && test -f /opt/kaloq-v1/migrate.env && mkdir /opt/kaloq-v1/releases/$RELEASE"
rsync -az -e 'ssh -o BatchMode=yes' "$TMP/" "$HOST:/opt/kaloq-v1/releases/$RELEASE/"
ssh -o BatchMode=yes "$HOST" bash -s -- "$RELEASE" <<'REMOTE'
set -euo pipefail
release="/opt/kaloq-v1/releases/$1"
previous="$(readlink -f /opt/kaloq-v1/current || true)"
chmod -R a+rX "$release"
install -d -m 700 /opt/kaloq-v1/backups
umask 077
runuser -u kaloq-v1-migrate -- pg_dump -Fc -d kaloq_v1_sandbox > "/opt/kaloq-v1/backups/$1.dump"
# One migration actor; additive schema remains in place on application rollback.
runuser -u kaloq-v1-migrate -- bash -c 'set -a; source /opt/kaloq-v1/migrate.env; set +a; cd "$1"; exec ./kaloq migrate' -- "$release"
runuser -u postgres -- psql -v ON_ERROR_STOP=1 -d kaloq_v1_sandbox -f "$release/deploy/runtime-grants.sql" >/dev/null
install -m 644 "$release/deploy/kaloq-v1.service" /etc/systemd/system/kaloq-v1.service
systemctl daemon-reload
ln -sfn "$release" /opt/kaloq-v1/current
healthy=0
if systemctl restart kaloq-v1; then
for attempt in 1 2 3 4 5; do
  if curl -fsS http://127.0.0.1:4102/healthz >/dev/null; then healthy=1; break; fi
  sleep 1
done
fi
if [ "$healthy" != 1 ]; then
  if [ -n "$previous" ] && [ -d "$previous" ]; then ln -sfn "$previous" /opt/kaloq-v1/current; systemctl restart kaloq-v1; else systemctl stop kaloq-v1; fi
  exit 1
fi
# Only the two previously inspected server blocks receive a new route include.
if ! bash "$release/deploy/install-route.sh" "$release"; then
  if [ -n "$previous" ] && [ -d "$previous" ]; then ln -sfn "$previous" /opt/kaloq-v1/current; systemctl restart kaloq-v1; else systemctl stop kaloq-v1; fi
  exit 1
fi
systemctl enable kaloq-v1 >/dev/null
REMOTE
curl --retry 2 --max-time 15 -fsS https://v-2077.com/kaloq-v1/healthz
printf '\nFormal release uploaded: %s\n' "$RELEASE"
