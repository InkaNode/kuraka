package provision

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/InkaNode/kuraka/internal/version"
)

func TestLibredeskVersionMatchesUpstream(t *testing.T) {
	data, err := os.ReadFile("../../patches/libredesk/UPSTREAM")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\nREF="+LibredeskVersion+"\n") {
		t.Fatalf("patches/libredesk/UPSTREAM does not pin REF=%s", LibredeskVersion)
	}
}

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
		mode := f.Mode().Perm()
		if mode != 0600 && (f.Name != "install.sh" || mode != 0700) {
			t.Fatalf("public secrets: %v", f.Mode())
		}
	}
	return out
}

func TestBundle(t *testing.T) {
	a, err := Bundle("../Á Team: ONE\n", "desk.example.com", Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Bundle("../Á Team: ONE\n", "desk.example.com", Options{})
	if err != nil {
		t.Fatal(err)
	}
	files, other := unzip(t, a.Zip), unzip(t, b.Zip)
	if len(files) != 6 {
		t.Fatalf("files: %v", files)
	}
	for _, name := range []string{"docker-compose.yml", "config.toml", ".env", "README.txt", "desk.caddy", "install.sh"} {
		if files[name] == "" {
			t.Fatalf("missing %s", name)
		}
	}
	for _, want := range []string{"name: kuraka-team-one", "ghcr.io/inkanode/libredesk:v2.9.2-kuraka." + version.Version, "postgres:17-alpine", "redis:7-alpine", "container_name: kuraka-team-one-desk", "external: true", "name: kuraka_mail", "--idempotent-install", "networks: [default, mail]"} {
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
	if pass != a.SystemPassword {
		t.Fatal("system password mismatch")
	}
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
	for _, want := range []string{"sh install.sh", "Activar", "Activate", "CNAME", "desk.example.com", "Kuraka > Integraciones", "ENGLISH", "ESPAÑOL", "solo usa la contraseña de System una vez", "only uses the System password once", "borre el zip descargado", "delete the downloaded zip"} {
		if !strings.Contains(files["README.txt"], want) {
			t.Errorf("README missing %s", want)
		}
	}
	if a.InternalURL != "http://kuraka-team-one-desk:9000" || strings.Contains(files["docker-compose.yml"], "ports:") {
		t.Fatal("app must use only the internal network")
	}
	if !strings.Contains(files["config.toml"], "disable_secure_cookies = false") {
		t.Fatal("insecure cookies")
	}
	if files["desk.caddy"] != "desk.example.com {\n\timport security_headers\n\timport kuraka_addon_proxy desk kuraka-team-one-desk:9000\n}\n" {
		t.Fatal("Caddy site mismatch")
	}
	for _, want := range []string{"set -eu", `cd "$(dirname "$0")"`, "KURAKA_DEPLOY=${KURAKA_DEPLOY:-/root/kuraka/deploy}", "docker compose up -d", "docker compose logs app | grep -q 'server starting'", `"$attempt" -ge 60`, "sleep 3", "exit 1", "sed -i '/^LIBREDESK_SYSTEM_USER_PASSWORD=/d' .env", `cp desk.caddy "$KURAKA_DEPLOY/sites/team-one-desk.caddy"`, `docker compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile`, "Listo.", "Done."} {
		if !strings.Contains(files["install.sh"], want) {
			t.Errorf("installer missing %s", want)
		}
	}
	install := files["install.sh"]
	up := strings.Index(install, "docker compose up -d")
	wait := strings.Index(install, "until docker compose logs app")
	remove := strings.Index(install, "sed -i")
	copySite := strings.Index(install, "cp desk.caddy")
	if !(up < wait && wait < remove && remove < copySite) {
		t.Fatal("installer must wait before removing password and publishing site")
	}

}

func TestBundleOptions(t *testing.T) {
	b, err := Bundle("", "desk.example.com", Options{MailNetwork: "lab_mail"})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b.Zip)
	if !strings.Contains(files["docker-compose.yml"], "name: lab_mail") {
		t.Fatal("options ignored")
	}
	for _, opts := range []Options{{MailNetwork: "bad\nnetwork"}, {MailNetwork: "${PASSWORD}"}} {
		if _, err := Bundle("test", "desk.example.com", opts); err == nil {
			t.Fatal("accepted invalid options")
		}
	}
}

func TestName(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"../Á Team: ONE\n", "team-one"}, {"", "tenant"}, {"---", "tenant"},
		{strings.Repeat("a", 49), strings.Repeat("a", 48)},
		{strings.Repeat("a", 47) + "-tail", strings.Repeat("a", 47)},
	} {
		if got := Name(tc.input); got != tc.want {
			t.Fatalf("Name(%q) = %q, want %q", tc.input, got, tc.want)
		}
		bundle, err := Bundle(tc.input, "desk.example.com", Options{})
		if err != nil || bundle.InternalURL != "http://kuraka-"+tc.want+"-desk:9000" {
			t.Fatalf("bundle name: %q %v", bundle.InternalURL, err)
		}
	}
}

func TestBundleSystemPassword(t *testing.T) {
	password := "Aa9!" + strings.Repeat("a0", 24)
	bundle, err := Bundle("test", "desk.example.com", Options{SystemPassword: password})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.SystemPassword != password || !strings.Contains(unzip(t, bundle.Zip)[".env"], "LIBREDESK_SYSTEM_USER_PASSWORD="+password+"\n") {
		t.Fatal("system password ignored")
	}
	for _, bad := range []string{password + "\n", password[:51], password + "0", strings.ToUpper(password), "${PASSWORD}", "weak"} {
		if _, err := Bundle("test", "desk.example.com", Options{SystemPassword: bad}); err == nil || err.Error() != "invalid bundle options" {
			t.Fatalf("invalid password: %v", err)
		}
	}
}
