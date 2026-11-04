package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"html/template"
	"path/filepath"

	"github.com/InkaNode/kuraka/internal/store"
)

var maintenancePage = template.Must(template.New("maintenance").Parse(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="20">
<title>{{.Name}}: mantenimiento</title>
<style>
body { margin: 0; padding: 32px; background: #f7f7f7; color: #222; font: 16px/1.6 system-ui, sans-serif; }
main { max-width: 560px; margin: 10vh auto; padding: 32px; background: #fff; border-top: 4px solid {{.Color}}; border-radius: 8px; }
img { display: block; max-width: 200px; max-height: 96px; margin-bottom: 24px; }
h1 { font-size: 24px; overflow-wrap: anywhere; }
.message { white-space: pre-wrap; overflow-wrap: anywhere; }
small { color: #555; }
</style>
</head>
<body>
<main>
{{if .Logo}}<img src="{{.Logo}}" alt="{{.Name}}">{{end}}
<h1>{{.Name}}: mantenimiento</h1>
{{if .Message}}<p class="message">{{.Message}}</p>{{else}}<p>Estamos actualizando {{.Name}}. Vuelve en unos minutos.<br>
{{.Name}} is being updated. Please check back in a few minutes.</p>{{end}}
<small>El correo sigue funcionando. / Email keeps working.</small>
</main>
</body>
</html>
`))

// writeMaintenancePage renders global Kuraka branding without external assets.
func (s *Server) writeMaintenancePage(ctx context.Context) error {
	if s.cfg.MaintenanceDir == "" {
		return nil
	}
	global, err := s.st.GetBranding(ctx, nil)
	if err != nil {
		return err
	}
	b := resolveBranding("kuraka", nil, global)
	if b.Name == "" {
		b.Name = "Kuraka"
	}
	if !brandingColor.MatchString(b.Color) {
		b.Color = "#a9502f"
	}
	var logo template.URL
	if b.Logo != "" {
		contentType, data, err := s.st.GetBrandingAsset(ctx, b.Logo)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil && len(data) <= 256<<10 {
			switch contentType {
			case "image/png", "image/jpeg", "image/webp", "image/svg+xml":
				logo = template.URL("data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data))
			}
		}
	}
	var out bytes.Buffer
	if err := maintenancePage.Execute(&out, struct {
		Name, Color, Message string
		Logo                 template.URL
	}{b.Name, b.Color, b.MaintenanceMessage, logo}); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.cfg.MaintenanceDir, "index.html"), out.Bytes())
}
