package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func TestProvisionInstall(t *testing.T) {
	h, _, _ := integrations(t)
	dir := t.TempDir()
	h.s.cfg.UpdatesDir = dir
	tenant := h.tenant("Install Team")
	body := fmt.Sprintf(`{"tenant_id":%d,"host":"desk.example.com","install":true}`, tenant)
	status, data, headers := h.root.raw("POST", "/helpdesks/provision", body, "http://kuraka.test", "application/json")
	if status != 201 || headers.Get("Content-Type") != "application/json" || headers.Get("Content-Disposition") != "" {
		t.Fatalf("install response: %d %v %s", status, headers, data)
	}
	got := object(t, data)
	if got["status"] != "pending" || got["dns"] != "manual" || got["url"] != "http://kuraka-install-team-desk:9000" {
		t.Fatalf("helpdesk: %s", data)
	}
	path := filepath.Join(dir, "install-install-team.json")
	request, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err := json.Unmarshal(request, &fields); err != nil || len(fields) != 3 || fields["name"] != "install-team" || fields["host"] != "desk.example.com" || !regexp.MustCompile(`^Aa9![0-9a-f]{48}$`).MatchString(fields["system_password"]) {
		t.Fatal("invalid install request")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("request mode: %v", info.Mode())
	}
	rows, err := h.st.ListHelpdesks(context.Background(), &tenant)
	if err != nil || len(rows) != 1 {
		t.Fatalf("helpdesks: %v %v", rows, err)
	}
	password, err := h.s.box.Open(rows[0].SetupSecretEnc)
	if err != nil || string(password) != fields["system_password"] || got["system_password"] != nil || got["setup_secret_enc"] != nil {
		t.Fatal("setup password mismatch or exposed")
	}
	h.root.do("POST", "/helpdesks/provision", map[string]any{"tenant_id": tenant, "host": "desk.example.com", "install": true}, 409)
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(request) {
		t.Fatal("conflict replaced install credentials")
	}
}

func TestProvisionInstallDisabled(t *testing.T) {
	h, _, _ := integrations(t)
	tenant := h.tenant("Disabled")
	body := h.root.do("POST", "/helpdesks/provision", map[string]any{"tenant_id": tenant, "host": "desk.example.com", "install": true}, 409)
	assertCode(t, body, "updates_disabled")
	if object(t, body)["error"].(map[string]any)["message"] != "KURAKA_UPDATES_DIR is not set" {
		t.Fatalf("disabled: %s", body)
	}
	rows, err := h.st.ListHelpdesks(context.Background(), &tenant)
	if err != nil || len(rows) != 0 {
		t.Fatalf("disabled install created helpdesk: %v", err)
	}
	h.root.do("POST", "/helpdesks/provision", map[string]any{"tenant_id": tenant, "host": "desk.example.com"}, 200)
}

func TestProvisionInstallWriteFailure(t *testing.T) {
	h, _, _ := integrations(t)
	h.s.cfg.UpdatesDir = filepath.Join(t.TempDir(), "missing")
	tenant := h.tenant("Failure")
	h.root.do("POST", "/helpdesks/provision", map[string]any{"tenant_id": tenant, "host": "desk.example.com", "install": true}, 500)
	rows, err := h.st.ListHelpdesks(context.Background(), &tenant)
	if err != nil || len(rows) != 0 {
		t.Fatalf("write failure left pending helpdesk: %v", err)
	}
}

func TestInstallStatus(t *testing.T) {
	h, _, dir, desk, other := updatesHarness(t)
	path := fmt.Sprintf("/helpdesks/%d/install", desk)
	h.root.do("GET", "/helpdesks/999/install", nil, 404)
	tenant := h.tenant("Tenant")
	h.admin(tenant, "install@admin.test").do("GET", path, nil, 403)
	deniedAudit(t, h.admin(tenant, "install2@admin.test"), "helpdesk.install")
	request := filepath.Join(dir, "install-acme.json")
	result := filepath.Join(dir, "install-acme.result.json")
	check := func(want, message string) {
		t.Helper()
		got := object(t, h.root.do("GET", path, nil, 200))
		if got["status"] != want || got["message"] != message || len(got) != 2 {
			t.Fatalf("status: %v, want %s %q", got, want, message)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	check("none", "")
	write(result, `{"status":"ok","message":""}`)
	check("ok", "")
	write(result, `{"status":"error","message":"already installed"}`)
	check("error", "already installed")
	write(request, `{"system_password":"secret"}`)
	check("installing", "")
	if got := object(t, h.root.do("GET", fmt.Sprintf("/helpdesks/%d/install", other), nil, 200)); got["status"] != "none" {
		t.Fatalf("connected helpdesk: %v", got)
	}
	h.s.cfg.UpdatesDir = ""
	check("none", "")
	h.s.cfg.UpdatesDir = dir
	if err := os.Remove(request); err != nil {
		t.Fatal(err)
	}
	write(result, `{"status":"invalid","message":"ignored"}`)
	check("none", "")
	write(result, `{"status":`)
	h.root.do("GET", path, nil, 500)
}

func TestActivatePending(t *testing.T) {
	for _, mode := range []string{"ok", "missing", "error", "installing", "not ready"} {
		t.Run(mode, func(t *testing.T) {
			h, _, hd := integrations(t)
			dir := t.TempDir()
			h.s.cfg.UpdatesDir = dir
			tenant := h.tenant("Automatic")
			h.root.do("POST", "/helpdesks/provision", map[string]any{"tenant_id": tenant, "host": "desk.example.com"}, 200)
			ctx := context.Background()
			rows, err := h.st.ListHelpdesks(ctx, &tenant)
			if err != nil || len(rows) != 1 {
				t.Fatalf("helpdesks: %v", err)
			}
			desk := rows[0]
			password, err := h.s.box.Open(desk.SetupSecretEnc)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "missing" {
				status := "ok"
				if mode == "error" {
					status = "error"
				}
				if err := os.WriteFile(filepath.Join(dir, "install-automatic.result.json"), []byte(fmt.Sprintf(`{"status":%q}`, status)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "installing" {
				if err := os.WriteFile(filepath.Join(dir, "install-automatic.json"), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "not ready" {
				hd.setupErr = errors.New("not listening")
			}
			h.s.activatePending(ctx)
			got, err := h.st.GetHelpdesk(ctx, desk.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "ok" {
				if got.Status != "pending" || got.SetupSecretEnc != desk.SetupSecretEnc || got.APIKey != "" {
					t.Fatal("unsuccessful install or activation changed pending helpdesk")
				}
				if mode != "not ready" && len(hd.setupPasswords) != 0 {
					t.Fatal("activation attempted without completed install")
				}
				return
			}
			if got.Status != "ready" || got.SetupSecretEnc != "" || got.APIKey != "generated-key" {
				t.Fatal("activation did not persist")
			}
			secret, err := h.s.box.Open(got.APISecretEnc)
			if err != nil || string(secret) != "generated-secret" {
				t.Fatal("API secret not encrypted")
			}
			if len(hd.setupPasswords) != 1 || hd.setupPasswords[0] != string(password) {
				t.Fatal("invalid setup password")
			}
			h.s.activatePending(ctx)
			if len(hd.setupPasswords) != 1 {
				t.Fatal("ready activation regenerated API key")
			}
		})
	}
}
