package api

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/InkaNode/kuraka/internal/store"
)

func TestMaintenancePage(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()
	// Disabled rendering does not need a store or a directory.
	if err := (&Server{}).writeMaintenancePage(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.cfg.MaintenanceDir = t.TempDir()
	readPage := func() string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(h.s.cfg.MaintenanceDir, "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	// Startup writes the page before the first delayed update check.
	ctx, cancel := context.WithCancel(ctx)
	done := h.s.StartUpdateChecks(ctx)
	cancel()
	<-done
	page := readPage()
	for _, text := range []string{
		`<html lang="es">`, `<meta http-equiv="refresh" content="20">`,
		`<title>Kuraka: mantenimiento</title>`, "#a9502f",
		"Estamos actualizando Kuraka. Vuelve en unos minutos.",
		"Kuraka is being updated. Please check back in a few minutes.",
		"El correo sigue funcionando. / Email keeps working.",
	} {
		if !strings.Contains(page, text) {
			t.Errorf("missing %q in %s", text, page)
		}
	}
	if strings.Contains(page, "<script") || strings.Contains(page, "<img") || strings.Contains(page, "https://") || strings.Contains(page, "http://") {
		t.Fatalf("page must be self-contained: %s", page)
	}
	logo := uploadAsset(t, h.root, "image/png", testPNG)
	h.root.do("PUT", "/branding/default", map[string]string{"name": "Shared", "color": "#123456", "logo": logo}, 200)
	page = readPage()
	if !strings.Contains(page, "<title>Shared: mantenimiento</title>") || !strings.Contains(page, "#123456") || !strings.Contains(page, dataURL("image/png", testPNG)) {
		t.Fatalf("shared branding: %s", page)
	}
	name, message := "<script>alert(1)</script>", "<script>alert(2)</script>\nSegunda linea & mas"
	h.root.do("PUT", "/branding/kuraka", map[string]string{"name": name, "maintenance_message": message}, 200)
	page = readPage()
	if strings.Contains(page, "<script>") || !strings.Contains(page, "&lt;script&gt;alert(1)&lt;/script&gt;") ||
		!strings.Contains(page, "&lt;script&gt;alert(2)&lt;/script&gt;\nSegunda linea &amp; mas") || strings.Contains(page, "Estamos actualizando") ||
		!strings.Contains(page, "#123456") || !strings.Contains(page, dataURL("image/png", testPNG)) {
		t.Fatalf("escaped custom branding: %s", page)
	}
	tenant := h.tenant("Acme")
	h.root.do("PUT", fmt.Sprintf("/branding/kuraka?tenant=%d", tenant), map[string]string{"name": "Tenant only", "maintenance_message": "Private"}, 200)
	if got := readPage(); got != page {
		t.Fatalf("tenant branding changed global page: %s", got)
	}
	if err := h.s.writeMaintenancePage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readPage(); got != page {
		t.Fatalf("renderer used tenant branding: %s", got)
	}
	// Rendering failures never fail a successful branding PUT or startup.
	h.s.cfg.MaintenanceDir = filepath.Join(t.TempDir(), "missing")
	if err := h.s.writeMaintenancePage(context.Background()); err == nil {
		t.Fatal("want missing directory error")
	}
	h.root.do("PUT", "/branding/kuraka", map[string]string{"name": "Still saved"}, 200)
	ctx, cancel = context.WithCancel(context.Background())
	done = h.s.StartUpdateChecks(ctx)
	cancel()
	<-done
}

func TestMaintenanceLogo(t *testing.T) {
	h := newHarness(t, false)
	h.s.cfg.MaintenanceDir = t.TempDir()
	ctx := context.Background()
	for i, tc := range []struct {
		name, contentType string
		size              int
		wantLogo          bool
	}{
		{"png", "image/png", 16, true},
		{"jpeg", "image/jpeg", 16, true},
		{"webp", "image/webp", 16, true},
		{"svg", "image/svg+xml", 16, true},
		{"limit", "image/png", 256 << 10, true},
		{"oversized", "image/png", (256 << 10) + 1, false},
		{"gif", "image/gif", 16, false},
		{"html", "text/html", 16, false},
		{"missing", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := fmt.Sprintf("%032x", i+1)
			data := []byte(strings.Repeat("x", tc.size))
			if tc.contentType != "" {
				if err := h.st.CreateBrandingAsset(ctx, id, tc.contentType, data); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.st.SetBranding(ctx, nil, "kuraka", store.Branding{Logo: id}); err != nil {
				t.Fatal(err)
			}
			if err := h.s.writeMaintenancePage(ctx); err != nil {
				t.Fatal(err)
			}
			page, err := os.ReadFile(filepath.Join(h.s.cfg.MaintenanceDir, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(page), "<img") != tc.wantLogo || strings.Contains(string(page), "#ZgotmplZ") {
				t.Fatalf("logo rendering: %s", page)
			}
			// html/template writes the "+" of image/svg+xml as &#43;.
			if tc.wantLogo && !strings.Contains(html.UnescapeString(string(page)), dataURL(tc.contentType, data)) {
				t.Fatal("logo was not inlined")
			}
		})
	}
}
