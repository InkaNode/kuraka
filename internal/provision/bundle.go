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
	HTTPPort    int
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
func Bundle(tenantName string, opts Options) ([]byte, error) {
	if opts.MailNetwork == "" {
		opts.MailNetwork = "kuraka_mail"
	}
	if opts.HTTPPort == 0 {
		opts.HTTPPort = 9000
	}
	if !networkName.MatchString(opts.MailNetwork) || opts.HTTPPort < 1 || opts.HTTPPort > 65535 {
		return nil, fmt.Errorf("invalid bundle options")
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
		return nil, err
	}
	dbPass, err := randomHex(24)
	if err != nil {
		return nil, err
	}
	systemPass, err := randomHex(24)
	if err != nil {
		return nil, err
	}
	systemPass = "Aa9!" + systemPass
	compose := fmt.Sprintf(`name: kuraka-%s
services:
  app:
    image: libredesk/libredesk:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:%d:9000"
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
  redis-data:
`, name, opts.HTTPPort, opts.MailNetwork)
	config := fmt.Sprintf(`[app]
env = "prod"
log_level = "info"
encryption_key = "%s"
[app.server]
address = "0.0.0.0:9000"
disable_secure_cookies = true
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
	readme := fmt.Sprintf(`Libredesk / Kuraka: %s

ESPAÑOL
La red Docker externa %s debe existir y permitir acceso a Stalwart.
Ejecute: docker compose up -d
Abra http://127.0.0.1:%d y entre como System usando
LIBREDESK_SYSTEM_USER_PASSWORD del archivo .env.
Cree una clave API de agente en Admin > Agents o mediante
POST /api/v1/agents/1/api-key, y registre URL, clave y secreto en
Kuraka > Integraciones para este tenant.
Los archivos .env y config.toml contienen secretos: guárdelos de forma privada.
Para publicar con HTTPS, use un proxy y cambie disable_secure_cookies a false.

ENGLISH
The external Docker network %s must exist and provide access to Stalwart.
Run: docker compose up -d
Open http://127.0.0.1:%d and log in as System with
LIBREDESK_SYSTEM_USER_PASSWORD from .env.
Create an agent API key in Admin > Agents or via
POST /api/v1/agents/1/api-key, then register the URL, key and secret in
Kuraka > Integraciones for this tenant.
Keep .env and config.toml private: they contain deployment secrets.
For HTTPS publication, use a proxy and set disable_secure_cookies to false.
`, name, opts.MailNetwork, opts.HTTPPort, opts.MailNetwork, opts.HTTPPort)
	files := []struct{ name, body string }{
		{"docker-compose.yml", compose}, {"config.toml", config},
		{".env", fmt.Sprintf("POSTGRES_PASSWORD=%s\nLIBREDESK_SYSTEM_USER_PASSWORD=%s\n", dbPass, systemPass)},
		{"README.txt", readme},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range files {
		h := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		h.SetMode(0600)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(file.body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
