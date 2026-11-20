// Package patches reads the patches Kuraka applies to Stalwart WebUI and
// Libredesk when it builds them (scripts/patches.sh explains the layout).
package patches

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed */UPSTREAM */series */*.patch
var files embed.FS

// Patch is one entry of an app's series. Missing* tell users what does not
// work while the patch is skipped.
type Patch struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	MissingEn string `json:"missing_en"`
	MissingEs string `json:"missing_es"`
	Skipped   bool   `json:"skipped"`
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
		lines := strings.Split(header, "\n")
		for i, l := range lines {
			key, value, _ := strings.Cut(l, ": ")
			switch key {
			case "Subject":
				// Long subjects fold onto lines that start with a space.
				for _, next := range lines[i+1:] {
					if !strings.HasPrefix(next, " ") {
						break
					}
					value += next
				}
				p.Title = patchTitle(value)
			case "Kuraka-Severity":
				p.Severity = value
			case "Kuraka-Missing-En":
				p.MissingEn = value
			case "Kuraka-Missing-Es":
				p.MissingEs = value
			}
		}
		if p.Severity != "urgent" && p.Severity != "normal" && p.Severity != "required" || p.MissingEn == "" || p.MissingEs == "" {
			return nil, fmt.Errorf("%s/%s: needs Kuraka-Severity (urgent, normal or required), Kuraka-Missing-En and Kuraka-Missing-Es", app, p.Name)
		}
		if p.Skipped && p.Severity == "required" {
			return nil, fmt.Errorf("%s/%s: required patches cannot be skipped", app, p.Name)
		}
		out = append(out, p)
	}
	return out, nil
}

// Upstream returns the upstream tag (REF in <app>/UPSTREAM) app is built from.
func Upstream(app string) string {
	data, _ := files.ReadFile(app + "/UPSTREAM")
	for _, line := range strings.Split(string(data), "\n") {
		if ref, ok := strings.CutPrefix(strings.TrimSpace(line), "REF="); ok {
			return ref
		}
	}
	return ""
}

// patchTitle turns "[PATCH] feat(tags): optional tag colors" into
// "Tags: optional tag colors".
func patchTitle(subject string) string {
	subject = strings.TrimSpace(strings.TrimPrefix(subject, "[PATCH] "))
	head, rest, ok := strings.Cut(subject, ": ")
	if !ok {
		return subject
	}
	if _, scope, ok := strings.Cut(strings.TrimSuffix(head, ")"), "("); ok && scope != "" {
		return strings.ToUpper(scope[:1]) + scope[1:] + ": " + rest
	}
	return rest
}
