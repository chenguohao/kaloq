#!/usr/bin/env bash
# Run on the authorized host after deploy, with an absolute release directory.
set -euo pipefail
release="${1:?release path required}"
[[ "$release" =~ ^/opt/kaloq-v1/releases/[a-zA-Z0-9_-]+$ ]] || exit 2
config=/etc/nginx/conf.d/castle.conf
backup="$(mktemp /etc/nginx/.kaloq-v1-config.XXXXXX)"
cp -p "$config" "$backup"
route_backup="$(mktemp /etc/nginx/.kaloq-v1-route.XXXXXX)"
had_route=0
if [ -f /etc/nginx/kaloq-v1.locations ]; then had_route=1; cp -p /etc/nginx/kaloq-v1.locations "$route_backup"; fi
rollback() {
  cp -p "$backup" "$config"
  if [ "$had_route" = 1 ]; then cp -p "$route_backup" /etc/nginx/kaloq-v1.locations; else rm -f /etc/nginx/kaloq-v1.locations; fi
}
trap 'rollback; rm -f "$backup" "$route_backup"' ERR
install -m 644 "$release/deploy/nginx.locations" /etc/nginx/kaloq-v1.locations
python3 - <<'PY'
from pathlib import Path
p=Path('/etc/nginx/conf.d/castle.conf');s=p.read_text()
needle='  include /etc/nginx/kaloq-demo.locations;'
added='  include /etc/nginx/kaloq-v1.locations;'
if added not in s:
    if s.count(needle)!=2: raise SystemExit('Expected two verified Demo include anchors; no route change made')
    p.write_text(s.replace(needle,needle+'\n'+added))
PY
nginx -t
systemctl reload nginx
trap - ERR
rm -f "$backup" "$route_backup"
