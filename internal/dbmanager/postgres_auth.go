package dbmanager

import (
	"strings"
)

// discoverPostgresPassword reads a postgres role password from .pgpass on the remote host.
func (p *PostgresManager) discoverPostgresPassword() string {
	script := `
set -e
for f in /root/.pgpass "$HOME/.pgpass"; do
  [ -r "$f" ] || continue
  line=$(awk -F: '!/^#/ && ($4=="postgres" || $4=="*") { print $5; exit }' "$f" 2>/dev/null || true)
  if [ -n "$line" ]; then printf '%s' "$line"; exit 0; fi
done
if command -v sudo >/dev/null 2>&1; then
  sudo -n -u postgres sh -c 'for f in "$HOME/.pgpass" /var/lib/postgresql/.pgpass; do
    [ -r "$f" ] || continue
    line=$(awk -F: "!/^#/ && (\$4==\"postgres\" || \$4==\"*\") { print \$5; exit }" "$f" 2>/dev/null || true)
    if [ -n "$line" ]; then printf "%s" "$line"; exit 0; fi
  done' 2>/dev/null || true
fi
`
	out := strings.TrimSpace(p.exec.RunQuiet("bash -lc " + shellQuote(strings.TrimSpace(script))))
	return out
}

func postgresPSQLArgs(password string) string {
	// -w: never prompt (non-interactive SSH)
	if password == "" {
		return "psql -w -t -A"
	}
	return "psql -w -h 127.0.0.1 -U postgres -t -A"
}

// postgresEnvPrefix clears client env vars that force the wrong DB role/host.
func postgresEnvPrefix(password string) string {
	base := "env -u PGUSER -u PGPASSWORD -u PGHOST -u PGPORT -u PGDATABASE -u PGCONNECT_TIMEOUT"
	if password == "" {
		return base
	}
	return base + " PGPASSWORD=" + shellQuote(password)
}
