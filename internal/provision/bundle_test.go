package provision

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func unzip(t *testing.T, b []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(data)
		if f.Mode().Perm() != 0600 {
			t.Fatalf("public secrets: %v", f.Mode())
		}
	}
	return out
}

func TestBundle(t *testing.T) {
	a, err := Bundle("../Á Team: ONE\n", Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Bundle("../Á Team: ONE\n", Options{})
	if err != nil {
		t.Fatal(err)
	}
	files, other := unzip(t, a), unzip(t, b)
	if len(files) != 4 {
		t.Fatalf("files: %v", files)
	}
	for _, name := range []string{"docker-compose.yml", "config.toml", ".env", "README.txt"} {
		if files[name] == "" {
			t.Fatalf("missing %s", name)
		}
	}
	for _, want := range []string{"name: kuraka-team-one", "libredesk/libredesk:latest", "postgres:17-alpine", "redis:7-alpine", "127.0.0.1:9000:9000", "external: true", "name: kuraka_mail", "--idempotent-install", "networks: [default, mail]"} {
		if !strings.Contains(files["docker-compose.yml"], want) {
			t.Errorf("missing %s", want)
		}
	}
	if files[".env"] == other[".env"] || files["config.toml"] == other["config.toml"] {
		t.Fatal("secrets reused")
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(files[".env"]), "\n") {
		k, v, _ := strings.Cut(line, "=")
		values[k] = v
	}
	pass := values["LIBREDESK_SYSTEM_USER_PASSWORD"]
	if len(pass) < 10 || !strings.ContainsAny(pass, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") || !strings.ContainsAny(pass, "abcdefghijklmnopqrstuvwxyz") || !strings.ContainsAny(pass, "0123456789") || !strings.ContainsAny(pass, "!@#$%^&*") {
		t.Fatal("weak system password")
	}
	if !strings.Contains(files["config.toml"], `password = "`+values["POSTGRES_PASSWORD"]+`"`) {
		t.Fatal("DB credentials mismatch")
	}
	for _, line := range strings.Split(files["config.toml"], "\n") {
		if strings.HasPrefix(line, "encryption_key") {
			_, key, _ := strings.Cut(line, " = ")
			key = strings.Trim(key, `"`)
			if _, err := hex.DecodeString(key); err != nil || len(key) != 32 {
				t.Fatal("invalid encryption key")
			}
		}
	}
	for _, want := range []string{"docker compose up -d", "System", "Admin > Agents", "POST /api/v1/agents/1/api-key", "Kuraka > Integraciones", "ENGLISH", "ESPAÑOL"} {
		if !strings.Contains(files["README.txt"], want) {
			t.Errorf("README missing %s", want)
		}
	}
}

func TestBundleOptions(t *testing.T) {
	b, err := Bundle("", Options{MailNetwork: "lab_mail", HTTPPort: 9876})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	if !strings.Contains(files["docker-compose.yml"], "127.0.0.1:9876:9000") || !strings.Contains(files["docker-compose.yml"], "name: lab_mail") {
		t.Fatal("options ignored")
	}
	for _, opts := range []Options{{HTTPPort: -1}, {HTTPPort: 65536}, {MailNetwork: "bad\nnetwork"}, {MailNetwork: "${PASSWORD}"}} {
		if _, err := Bundle("test", opts); err == nil {
			t.Fatal("accepted invalid options")
		}
	}
}
