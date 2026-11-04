// Package provision produces isolated Libredesk deployment bundles.
package provision

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

type Options struct {
	MailNetwork string
}

type Result struct {
	Zip            []byte
	InternalURL    string
	SystemPassword string
}

var unsafeName = regexp.MustCompile(`[^a-z0-9]+`)
var networkName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Bundle returns a zip with fresh credentials on every call. The service and
// configuration keys follow Libredesk's upstream compose and config sample.
func Bundle(tenantName, host string, opts Options) (Result, error) {
	if opts.MailNetwork == "" {
		opts.MailNetwork = "kuraka_mail"
	}
	if !networkName.MatchString(opts.MailNetwork) {
		return Result{}, fmt.Errorf("invalid bundle options")
	}
	name := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(tenantName), "-"), "-")
	if len(name) > 48 {
		name = strings.TrimRight(name[:48], "-")
	}
	if name == "" {
		name = "tenant"
	}
	key, err := randomHex(16)
	if err != nil {
		return Result{}, err
	}
	dbPass, err := randomHex(24)
	if err != nil {
		return Result{}, err
	}
	systemPass, err := randomHex(24)
	if err != nil {
		return Result{}, err
	}
	systemPass = "Aa9!" + systemPass
	// Labels the database volume: Postgres keeps the password of the first
	// install, so install.sh refuses to run another bundle over it.
	bundleID, err := randomHex(8)
	if err != nil {
		return Result{}, err
	}
	compose := fmt.Sprintf(`name: kuraka-%s
services:
  app:
    # Update deliberately; --upgrade runs on every start.
    image: libredesk/libredesk:v2.9.2
    container_name: kuraka-%s-desk
    restart: unless-stopped
    environment:
      LIBREDESK_SYSTEM_USER_PASSWORD: ${LIBREDESK_SYSTEM_USER_PASSWORD}
    networks: [default, mail]
    depends_on:
      db:
        condition: service_healthy
      redis:
        condition: service_started
    volumes:
      - uploads:/libredesk/uploads
      - ./config.toml:/libredesk/config.toml:ro
    command: [sh, -c, "./libredesk --install --idempotent-install --yes --config /libredesk/config.toml && ./libredesk --upgrade --yes --config /libredesk/config.toml && ./libredesk --config /libredesk/config.toml"]
  db:
    image: postgres:17-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: libredesk
      POSTGRES_DB: libredesk
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    healthcheck:
      test: [CMD-SHELL, "pg_isready -U libredesk -d libredesk"]
      interval: 10s
      timeout: 5s
      retries: 6
    volumes:
      - postgres-data:/var/lib/postgresql/data
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    volumes:
      - redis-data:/data
networks:
  mail:
    external: true
    name: %s
volumes:
  uploads:
  postgres-data:
    labels:
      kuraka.bundle: %s
  redis-data:
`, name, name, opts.MailNetwork, bundleID)
	config := fmt.Sprintf(`[app]
env = "prod"
log_level = "info"
encryption_key = "%s"
[app.server]
address = "0.0.0.0:9000"
disable_secure_cookies = false
session_lifetime = "9h"
read_timeout = "60s"
write_timeout = "60s"
max_body_size = 104857600
read_buffer_size = 65536
keepalive_timeout = "10s"
[auth]
local_login_enabled = true
[db]
host = "db"
port = 5432
user = "libredesk"
password = "%s"
database = "libredesk"
ssl_mode = "disable"
max_open = 30
max_idle = 30
max_lifetime = "300s"
[redis]
address = "redis:6379"
db = 0
[upload]
provider = "fs"
[upload.fs]
upload_path = "uploads"
expiry = "1h"
`, key, dbPass)
	caddy := fmt.Sprintf("%s {\n\timport security_headers\n\treverse_proxy kuraka-%s-desk:9000\n}\n", host, name)
	install := fmt.Sprintf(`#!/bin/sh
set -eu
cd "$(dirname "$0")"
KURAKA_DEPLOY=${KURAKA_DEPLOY:-/root/kuraka/deploy}
VOLUME=kuraka-%[1]s_postgres-data
if docker volume inspect "$VOLUME" >/dev/null 2>&1 &&
	[ "$(docker volume inspect -f '{{index .Labels "kuraka.bundle"}}' "$VOLUME")" != %[2]s ]; then
	echo 'Ya hay un Libredesk de otro zip. Use ese zip, o borre la instalación con docker compose down -v (borra sus datos).' >&2
	echo 'A Libredesk from another zip exists. Use that zip, or remove it with docker compose down -v (deletes its data).' >&2
	exit 1
fi
docker compose up -d
cp desk.caddy "$KURAKA_DEPLOY/sites/%[1]s-desk.caddy"
(cd "$KURAKA_DEPLOY" && docker compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile)
printf '%%s\n' 'Listo. En Kuraka > Integraciones pulse Activar.' 'Done. In Kuraka > Integrations press Activate.'
`, name, bundleID)
	readme := fmt.Sprintf(`Libredesk / Kuraka: %s

ESPAÑOL
Descomprima el ZIP en el servidor y ejecute: sh install.sh
Luego, en Kuraka > Integraciones pulse Activar.
El host %s debe apuntar al servidor. Kuraka crea el CNAME DNS cuando
el dominio tiene Cloudflare; en otro caso, créelo manualmente.
La red Docker externa %s debe existir y permitir acceso a Stalwart.
Los archivos .env y config.toml contienen secretos: guárdelos de forma privada.

ENGLISH
Unzip on the server and run: sh install.sh
Then, in Kuraka > Integrations press Activate.
Host %s must point to the server. Kuraka creates the DNS CNAME when
the domain has Cloudflare; otherwise create it manually.
The external Docker network %s must exist and provide access to Stalwart.
Keep .env and config.toml private: they contain deployment secrets.
`, name, host, opts.MailNetwork, host, opts.MailNetwork)
	files := []struct{ name, body string }{
		{"docker-compose.yml", compose}, {"config.toml", config},
		{".env", fmt.Sprintf("POSTGRES_PASSWORD=%s\nLIBREDESK_SYSTEM_USER_PASSWORD=%s\n", dbPass, systemPass)},
		{"README.txt", readme}, {"desk.caddy", caddy}, {"install.sh", install},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range files {
		h := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		h.SetMode(0600)
		if file.name == "install.sh" {
			h.SetMode(0700)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return Result{}, err
		}
		if _, err := w.Write([]byte(file.body)); err != nil {
			return Result{}, err
		}
	}
	if err := zw.Close(); err != nil {
		return Result{}, err
	}
	return Result{Zip: buf.Bytes(), InternalURL: "http://kuraka-" + name + "-desk:9000", SystemPassword: systemPass}, nil
}
