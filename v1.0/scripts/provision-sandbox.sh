#!/usr/bin/env bash
# Run on the authorized test host as root. Creates only the formal sandbox.
set -euo pipefail
for name in kaloq-v1 kaloq-v1-migrate; do
  id "$name" >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin "$name"
done
install -d -m 755 /opt/kaloq-v1 /opt/kaloq-v1/releases
for name in kaloq-v1 kaloq-v1-migrate; do
  if ! runuser -u postgres -- psql -Atc "SELECT 1 FROM pg_roles WHERE rolname='$name'" | grep -qx 1; then
    runuser -u postgres -- createuser "$name"
  fi
done
if ! runuser -u postgres -- psql -Atc "SELECT 1 FROM pg_database WHERE datname='kaloq_v1_sandbox'" | grep -qx 1; then
  runuser -u postgres -- createdb -O kaloq-v1-migrate kaloq_v1_sandbox
fi
runuser -u postgres -- psql -v ON_ERROR_STOP=1 -d kaloq_v1_sandbox <<'SQL'
REVOKE ALL ON DATABASE kaloq_v1_sandbox FROM PUBLIC;
GRANT CONNECT ON DATABASE kaloq_v1_sandbox TO "kaloq-v1";
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO "kaloq-v1-migrate";
SQL
python3 - <<'PY'
import base64, os, pathlib
root=pathlib.Path('/opt/kaloq-v1')
runtime=root/'runtime.env'
if not runtime.exists():
    fd=os.open(runtime,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as f:
        f.write('DATABASE_URL=postgres:///kaloq_v1_sandbox?host=/var/run/postgresql\n')
        f.write('TOTP_ENCRYPTION_KEY='+base64.b64encode(os.urandom(32)).decode()+'\n')
        f.write('PUBLIC_ORIGIN=https://v-2077.com\nCOOKIE_PATH=/kaloq-v1/\nLISTEN_ADDR=127.0.0.1:4102\nWEB_DIR=/opt/kaloq-v1/current/web\nTRUST_PROXY_LOOPBACK=true\n')
migration=root/'migrate.env'
if not migration.exists():
    fd=os.open(migration,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as f:
        f.write('DATABASE_URL=postgres:///kaloq_v1_sandbox?host=/var/run/postgresql\nMIGRATIONS_DIR=database/migrations\n')
PY
chown root:kaloq-v1 /opt/kaloq-v1/runtime.env
chown root:kaloq-v1-migrate /opt/kaloq-v1/migrate.env
chmod 640 /opt/kaloq-v1/runtime.env /opt/kaloq-v1/migrate.env
