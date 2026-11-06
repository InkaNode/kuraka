// Package patches reads the patches Kuraka applies to Stalwart WebUI and
// Libredesk when it builds them (scripts/patches.sh explains the layout).
package patches

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed */series */*.patch
var files embed.FS

// Patch is one entry of an app's series. Missing* tell users what does not
// work while the patch is skipped.
type Patch struct {
	Name      string `json:"name"`
	Severity  string `json:"severity"`
	MissingEn string `json:"missing_en"`
	MissingEs string `json:"missing_es"`
	Skipped   bool   `json:"-"`
}

// Series lists app's patches in apply order.
func Series(app string) ([]Patch, error) {
	data, err := files.ReadFile(app + "/series")
	if err != nil {
		return nil, err
	}
	var out []Patch
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p := Patch{Name: strings.TrimPrefix(line, "-"), Skipped: strings.HasPrefix(line, "-")}
		body, err := files.ReadFile(app + "/" + p.Name)
		if err != nil {
			return nil, err
		}
		header, _, _ := strings.Cut(string(body), "\n---\n")
		for _, l := range strings.Split(header, "\n") {
			key, value, _ := strings.Cut(l, ": ")
			switch key {
			case "Kuraka-Severity":
				p.Severity = value
			case "Kuraka-Missing-En":
				p.MissingEn = value
			case "Kuraka-Missing-Es":
				p.MissingEs = value
			}
		}
		if p.Severity != "urgent" && p.Severity != "normal" || p.MissingEn == "" || p.MissingEs == "" {
			return nil, fmt.Errorf("%s/%s: needs Kuraka-Severity (urgent or normal), Kuraka-Missing-En and Kuraka-Missing-Es", app, p.Name)
		}
		out = append(out, p)
	}
	return out, nil
}

// Skipped lists app's patches this build left out.
func Skipped(app string) ([]Patch, error) {
	series, err := Series(app)
	if err != nil {
		return nil, err
	}
	out := []Patch{}
	for _, p := range series {
		if p.Skipped {
			out = append(out, p)
		}
	}
	return out, nil
}
